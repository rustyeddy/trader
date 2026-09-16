package external

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

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
//
// v1 assumes one strategy identity per connection (ADR-062); grpcServer
// enforces that only one session is active — from a successful
// Handshake until its Run stream ends or its admission timeout
// expires — at any time, rejecting a concurrent Handshake outright
// rather than queuing or silently replacing the active one.
type grpcServer struct {
	v1.UnimplementedStrategyHostServiceServer

	logger           *slog.Logger
	callbackTimeout  time.Duration
	admissionTimeout time.Duration

	mu       sync.Mutex
	sessions map[string]*runSession

	// pending is the most recently Handshake'd strategy.Strategy not
	// yet claimed via Host.Strategy, or nil. notify is closed and
	// replaced every time pending changes (set or cleared), so
	// Host.Strategy can wait for the next change without polling.
	// This — rather than a fixed-size buffered channel — is what lets
	// an abandoned session's own admission-timeout cleanup correctly
	// withdraw a strategy nobody ever claimed, instead of leaving a
	// stale entry that would silently block every later session
	// (review finding).
	pending strategy.Strategy
	notify  chan struct{}
}

func newGRPCServer(logger *slog.Logger, callbackTimeout, admissionTimeout time.Duration) *grpcServer {
	return &grpcServer{
		logger:           logger,
		callbackTimeout:  callbackTimeout,
		admissionTimeout: admissionTimeout,
		sessions:         make(map[string]*runSession),
		notify:           make(chan struct{}),
	}
}

// Handshake validates the guest's declared protocol version, decodes
// its StrategyDescriptor, negotiates capabilities, and — on success —
// mints a session and makes the resulting strategy.Strategy available
// via Host.Strategy. It rejects a Handshake while any other session
// is already active (registered in g.sessions), whether or not that
// session's own strategy has been claimed yet.
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

	sess := newRunSession(sessionID, descriptor, negotiated, g.callbackTimeout)
	strat := newExternalStrategyAdapter(sess, g.logger)

	g.mu.Lock()
	if len(g.sessions) > 0 {
		g.mu.Unlock()
		return nil, status.Error(codes.FailedPrecondition,
			"external: handshake: a session is already active; v1 supports one strategy identity per connection at a time")
	}
	g.sessions[sessionID] = sess
	g.setPendingLocked(strat)
	g.mu.Unlock()

	g.logger.InfoContext(ctx, "external strategy handshake accepted",
		"session_id", sessionID, "strategy", descriptor.Name, "strategy_version", descriptor.Version)

	if g.admissionTimeout > 0 {
		go g.expireIfNeverBound(sessionID, sess, strat)
	}

	return &v1.HandshakeResponse{
		Accepted:        true,
		ProtocolVersion: v1.ProtocolVersion,
		Capabilities:    negotiated,
		SessionId:       sessionID,
	}, nil
}

// setPendingLocked replaces g.pending with strat and wakes every
// Host.Strategy call waiting on the previous notify channel. g.mu
// must already be held.
func (g *grpcServer) setPendingLocked(strat strategy.Strategy) {
	g.pending = strat
	close(g.notify)
	g.notify = make(chan struct{})
}

// takePending claims and clears g.pending, if set, returning it and
// true; otherwise returns the current notify channel to wait on.
func (g *grpcServer) takePending() (strategy.Strategy, <-chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pending != nil {
		strat := g.pending
		g.pending = nil
		return strat, nil
	}
	return nil, g.notify
}

// expireIfNeverBound frees sessionID's slot if its own Run stream
// never binds within g.admissionTimeout — a successful Handshake is
// not guaranteed to be followed by a Run call (the guest may
// disconnect, or the composition root driving Host.Strategy/Start may
// itself be canceled first), and without this an abandoned Handshake
// would hold this host's one session slot indefinitely (review
// finding). If strat was never claimed via Host.Strategy either, it
// is withdrawn from g.pending too.
func (g *grpcServer) expireIfNeverBound(sessionID string, sess *runSession, strat strategy.Strategy) {
	select {
	case <-sess.streamReady:
		return
	case <-time.After(g.admissionTimeout):
	}

	g.mu.Lock()
	if g.pending == strat {
		g.pending = nil
	}
	delete(g.sessions, sessionID)
	g.mu.Unlock()

	sess.close()
	g.logger.Warn("external strategy session expired before its run stream opened",
		"session_id", sessionID, "admission_timeout", g.admissionTimeout)
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
// It ends when the guest closes its send side (io.EOF), a transport
// error occurs, an invalid message arrives, or this session's own
// forceTeardown fires — a fired callback-supervision timer (a
// non-nil teardown reason) ends the RPC with an error status; a
// deliberate, graceful end (Close, a nil reason) ends it cleanly.
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

	type recvResult struct {
		msg *v1.RunClientMessage
		err error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			recvCh <- recvResult{msg, err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case reason := <-sess.teardown:
			if reason != nil {
				return status.Error(codes.DeadlineExceeded, reason.Error())
			}
			return nil

		case res := <-recvCh:
			if res.err != nil {
				if errors.Is(res.err, io.EOF) {
					return nil
				}
				return res.err
			}

			switch payload := res.msg.GetPayload().(type) {
			case *v1.RunClientMessage_OnBarResponse:
				if payload.OnBarResponse == nil {
					return status.Error(codes.InvalidArgument, "external: run: on_bar_response payload must not be nil")
				}
				sess.dispatch(payload.OnBarResponse.GetSequence(), res.msg)
			case *v1.RunClientMessage_OnFillResponse:
				if payload.OnFillResponse == nil {
					return status.Error(codes.InvalidArgument, "external: run: on_fill_response payload must not be nil")
				}
				sess.dispatch(payload.OnFillResponse.GetSequence(), res.msg)
			case *v1.RunClientMessage_RunOpen:
				return status.Error(codes.InvalidArgument, "external: run: run_open must be the stream's first message only")
			default:
				return status.Error(codes.InvalidArgument, "external: run: message carries no recognized payload")
			}
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
		return nil, status.Errorf(codes.FailedPrecondition,
			"external: get history bars: callback %d is not an in-flight on-bar callback", q.CallbackSequence)
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
