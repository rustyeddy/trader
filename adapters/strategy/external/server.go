package external

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/strategy"
)

// grpcServer implements v1.StrategyHostServiceServer, translating
// wire calls into runSession state transitions. It is not exported:
// callers interact with this package through Host.
//
// Handshake, Run, and GetHistoryBars implement exactly the
// session-binding contract strategy.proto's own doc comments state:
// Handshake issues a session_id; Run's own first message (RunOpen)
// must name it before the host writes anything else; GetHistoryBars
// must name it, plus a callback_sequence naming a callback this
// session is still waiting on a response for.
type grpcServer struct {
	v1.UnimplementedStrategyHostServiceServer

	logger *slog.Logger

	mu       sync.Mutex
	sessions map[string]*runSession

	// ready carries one successfully Handshake'd strategy.Strategy per
	// send, consumed by Host.Strategy. It is deliberately unbuffered by
	// more than one entry: v1 assumes one strategy identity per
	// connection (ADR-062), so a second Handshake before the first's
	// strategy has been claimed is rejected rather than silently
	// queued or replacing it (see Handshake below).
	ready chan strategy.Strategy
}

func newGRPCServer(logger *slog.Logger) *grpcServer {
	return &grpcServer{
		logger:   logger,
		sessions: make(map[string]*runSession),
		ready:    make(chan strategy.Strategy, 1),
	}
}

// Handshake validates the guest's declared protocol version, decodes
// its StrategyDescriptor, negotiates capabilities, and — on success —
// mints a session and makes the resulting strategy.Strategy available
// via Host.Strategy.
func (g *grpcServer) Handshake(ctx context.Context, req *v1.HandshakeRequest) (*v1.HandshakeResponse, error) {
	if req.GetProtocolVersion() != v1.ProtocolVersion {
		return &v1.HandshakeResponse{
			Accepted:        false,
			ProtocolVersion: v1.ProtocolVersion,
			RejectReason: ToWireError(v1.ErrorCode_ERROR_CODE_PROTOCOL_VERSION_MISMATCH,
				fmt.Errorf("host protocol version %q, guest requested %q", v1.ProtocolVersion, req.GetProtocolVersion())),
		}, nil
	}

	descriptor, err := FromWireDescriptor(req.GetStrategyDescriptor())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "external: handshake: %v", err)
	}

	negotiated := negotiateCapabilities(req.GetCapabilities())

	sessionID, err := newSessionID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "external: handshake: %v", err)
	}

	sess := newRunSession(sessionID, descriptor, negotiated)
	strat := newExternalStrategyAdapter(sess, g.logger)

	select {
	case g.ready <- strat:
	default:
		return nil, status.Error(codes.FailedPrecondition,
			"external: handshake: a strategy from a previous handshake is still pending; v1 supports one strategy identity per connection at a time")
	}

	g.mu.Lock()
	g.sessions[sessionID] = sess
	g.mu.Unlock()

	g.logger.InfoContext(ctx, "external strategy handshake accepted",
		"session_id", sessionID, "strategy", descriptor.Name, "strategy_version", descriptor.Version)

	return &v1.HandshakeResponse{
		Accepted:        true,
		ProtocolVersion: v1.ProtocolVersion,
		Capabilities:    negotiated,
		SessionId:       sessionID,
	}, nil
}

// negotiateCapabilities returns the subset of requested this host
// supports, deduplicated and in a stable order. v1's only defined
// capability is CAPABILITY_FILL_HANDLER (strategy.proto's own
// Capability doc comment); any other value (including
// CAPABILITY_UNSPECIFIED or a future value this host build predates)
// is silently dropped rather than rejected, matching v1's own
// additive-capability compatibility rule.
func negotiateCapabilities(requested []v1.Capability) []v1.Capability {
	var out []v1.Capability
	seen := false
	for _, c := range requested {
		if c == v1.Capability_CAPABILITY_FILL_HANDLER && !seen {
			seen = true
			out = append(out, c)
		}
	}
	return out
}

// Run is the guest-opened, bidirectional, long-lived session stream.
func (g *grpcServer) Run(stream v1.StrategyHostService_RunServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	runOpen := first.GetRunOpen()
	if runOpen == nil {
		return status.Error(codes.InvalidArgument, "external: run: first message must be run_open")
	}
	sessionID, err := FromWireRunOpen(runOpen)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "external: run: %v", err)
	}

	sess, ok := g.session(sessionID)
	if !ok {
		return status.Errorf(codes.NotFound, "external: run: unknown session %q", sessionID)
	}
	if err := sess.bindStream(stream); err != nil {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	defer sess.close()
	defer g.deleteSession(sessionID)

	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		switch payload := msg.GetPayload().(type) {
		case *v1.RunClientMessage_OnBarResponse:
			sess.dispatch(payload.OnBarResponse.GetSequence(), msg)
		case *v1.RunClientMessage_OnFillResponse:
			sess.dispatch(payload.OnFillResponse.GetSequence(), msg)
		case *v1.RunClientMessage_RunOpen:
			return status.Error(codes.InvalidArgument, "external: run: run_open must be the stream's first message only")
		default:
			return status.Error(codes.InvalidArgument, "external: run: message carries no recognized payload")
		}
	}
}

// GetHistoryBars answers a guest's scoped history lookback request
// against the exact frozen View the named in-flight callback was
// built from — never whatever the host's live state happens to be
// when the RPC arrives (ADR-062's own corrected View design).
func (g *grpcServer) GetHistoryBars(_ context.Context, req *v1.GetHistoryBarsRequest) (*v1.GetHistoryBarsResponse, error) {
	q, err := FromWireHistoryBarsRequest(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "external: get history bars: %v", err)
	}

	sess, ok := g.session(q.SessionID)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "external: get history bars: unknown session %q", q.SessionID)
	}

	view, ok := sess.viewFor(q.CallbackSequence)
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "external: get history bars: callback %d is not in flight", q.CallbackSequence)
	}

	declared := false
	for _, r := range sess.descriptor.Requirements {
		if r.Instrument.Equal(q.Instrument) && r.Interval == q.Interval {
			declared = true
			break
		}
	}
	if !declared {
		return nil, status.Error(codes.FailedPrecondition,
			"external: get history bars: instrument/interval was not declared in this session's own handshake requirements")
	}

	history, ok := view.(strategy.History)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "external: get history bars: this run's view does not support history lookback")
	}

	bars, ok := history.HistoryBars(q.Instrument, q.Interval, q.Count)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition,
			"external: get history bars: instrument/interval was not declared as one of this strategy's own requirements")
	}

	return ToWireHistoryBarsResponse(bars), nil
}

func (g *grpcServer) session(id string) (*runSession, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	sess, ok := g.sessions[id]
	return sess, ok
}

func (g *grpcServer) deleteSession(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.sessions, id)
}
