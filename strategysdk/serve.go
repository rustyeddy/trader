package strategysdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// SocketPathEnv is the environment variable Serve reads to find the
// Unix-domain socket it must dial — the exact contract
// adapters/strategy/external.Process sets on the child process it
// launches (ADR-063). Mirrored here, not imported from that package,
// since neither side of the v1 boundary imports the other (the
// package doc comment).
const SocketPathEnv = "TRADER_STRATEGY_SOCKET"

// Serve is the intended entry point for an external strategy binary's
// own main():
//
//	func main() {
//	    if err := strategysdk.Serve(mystrategy.New(cfg)); err != nil {
//	        log.Fatal(err)
//	    }
//	}
//
// It reads the Unix-domain socket path from SocketPathEnv, dials it,
// performs the v1 Handshake, and drives strat through the Run stream
// until the host ends the session, the connection fails, or this
// process receives SIGINT/SIGTERM — whichever happens first. Serve
// blocks until one of those occurs, then returns.
func Serve(strat Strategy) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return ServeContext(ctx, strat)
}

// ServeContext is Serve without built-in OS signal handling: it reads
// SocketPathEnv, dials, and drives strat until the host ends the
// session, the connection fails, or ctx is done.
func ServeContext(ctx context.Context, strat Strategy) error {
	sockPath := os.Getenv(SocketPathEnv)
	if sockPath == "" {
		return fmt.Errorf("strategysdk: %s is not set — this process must be launched by a Trader host (ADR-063)", SocketPathEnv)
	}

	conn, err := grpc.NewClient("unix:"+sockPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("strategysdk: dialing %s: %w", sockPath, err)
	}
	defer func() { _ = conn.Close() }()

	return ServeConn(ctx, conn, strat)
}

// ServeConn drives strat through Strategy Protocol v1 over an
// already-established gRPC connection — the lower-level entry point
// ServeContext itself uses after dialing, exposed directly so a
// caller (typically a test) can supply its own grpc.ClientConnInterface,
// for example one backed by an in-process bufconn listener, without
// needing a real Unix-domain socket.
func ServeConn(ctx context.Context, conn grpc.ClientConnInterface, strat Strategy) error {
	client := v1.NewStrategyHostServiceClient(conn)

	capabilities := negotiatedCapabilities(strat)

	wireDescriptor, err := toWireDescriptor(strat.Describe())
	if err != nil {
		return fmt.Errorf("strategysdk: describe: %w", err)
	}

	hsResp, err := client.Handshake(ctx, &v1.HandshakeRequest{
		ProtocolVersion:    v1.ProtocolVersion,
		StrategyDescriptor: wireDescriptor,
		Capabilities:       capabilities,
	})
	if err != nil {
		return fmt.Errorf("strategysdk: handshake: %w", err)
	}
	if !hsResp.GetAccepted() {
		if rejectErr := fromWireError(hsResp.GetRejectReason()); rejectErr != nil {
			return fmt.Errorf("strategysdk: handshake rejected: %w", rejectErr)
		}
		return fmt.Errorf("strategysdk: handshake rejected: host protocol version %q, this SDK is %q",
			hsResp.GetProtocolVersion(), v1.ProtocolVersion)
	}

	stream, err := client.Run(ctx)
	if err != nil {
		return fmt.Errorf("strategysdk: opening run stream: %w", err)
	}
	if err := stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_RunOpen{
		RunOpen: &v1.RunOpen{SessionId: hsResp.GetSessionId()},
	}}); err != nil {
		return fmt.Errorf("strategysdk: sending run_open: %w", err)
	}

	g := &guestRun{
		ctx:          ctx,
		client:       client,
		stream:       stream,
		sessionID:    hsResp.GetSessionId(),
		strat:        strat,
		fillHandler:  asFillHandler(strat),
		requirements: strat.Describe().Requirements,
	}
	return g.loop()
}

// negotiatedCapabilities returns the v1.Capability set strat itself
// advertises at Handshake, determined entirely by which optional
// strategysdk interfaces it implements — mirroring how
// ExternalStrategyAdapter's own capability wrapper family is selected
// host-side (ADR-062).
func negotiatedCapabilities(strat Strategy) []v1.Capability {
	if _, ok := strat.(FillHandler); ok {
		return []v1.Capability{v1.Capability_CAPABILITY_FILL_HANDLER}
	}
	return nil
}

func asFillHandler(strat Strategy) FillHandler {
	fh, _ := strat.(FillHandler)
	return fh
}

// guestRun holds the per-session state ServeConn's own Run-stream
// loop needs — the guest-side counterpart to
// adapters/strategy/external's own runSession, but far simpler: there
// is exactly one strategy, one stream, and no concurrent callers here
// (the loop below is the only goroutine that ever touches this
// state), so none of that package's own actor/channel machinery is
// needed.
type guestRun struct {
	ctx          context.Context
	client       v1.StrategyHostServiceClient
	stream       v1.StrategyHostService_RunClient
	sessionID    string
	strat        Strategy
	fillHandler  FillHandler // nil unless strat implements FillHandler
	requirements []DataRequirement

	clock *hostClock // set once SessionStart arrives
	runID string
}

// loop drives the Run stream until the host ends the session
// (SessionEnd), the stream itself ends (EOF/transport error), or ctx
// is done.
func (g *guestRun) loop() error {
	for {
		msg, err := g.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if g.ctx.Err() != nil {
				return g.ctx.Err()
			}
			return fmt.Errorf("strategysdk: run stream: %w", err)
		}

		switch payload := msg.GetPayload().(type) {
		case *v1.RunServerMessage_SessionStart:
			if err := g.handleSessionStart(payload.SessionStart); err != nil {
				return err
			}
		case *v1.RunServerMessage_BarEvent:
			if err := g.handleBarEvent(payload.BarEvent); err != nil {
				return err
			}
		case *v1.RunServerMessage_FillEvent:
			if err := g.handleFillEvent(payload.FillEvent); err != nil {
				return err
			}
		case *v1.RunServerMessage_SessionEnd:
			return sessionEndErr(payload.SessionEnd)
		default:
			return fmt.Errorf("strategysdk: run stream: received message with no recognized payload")
		}
	}
}

func (g *guestRun) handleSessionStart(w *v1.SessionStart) error {
	if w == nil {
		return fmt.Errorf("%w: session_start must be set", ErrInvalidWireValue)
	}
	g.runID = w.GetRunId()
	g.clock = newHostClock(time.Unix(0, w.GetStartTimeUnixNanos()).UTC())

	env := Environment{
		Clock:  g.clock,
		RunID:  g.runID,
		Logger: slog.New(slog.DiscardHandler),
	}
	if err := g.strat.Start(g.ctx, env); err != nil {
		return fmt.Errorf("strategysdk: start: %w", err)
	}
	return nil
}

func (g *guestRun) handleBarEvent(w *v1.BarEvent) error {
	if w == nil {
		return fmt.Errorf("%w: bar_event must be set", ErrInvalidWireValue)
	}
	event, err := fromWireBarEvent(w)
	if err != nil {
		return fmt.Errorf("strategysdk: bar event: %w", err)
	}
	if g.clock != nil {
		g.clock.set(event.Bar.Time)
	}
	account, err := fromWireAccountSnapshot(w.GetAccount())
	if err != nil {
		return fmt.Errorf("strategysdk: bar event: %w", err)
	}

	view := &guestView{
		ctx:          g.ctx,
		client:       g.client,
		sessionID:    g.sessionID,
		sequence:     w.GetSequence(),
		account:      account,
		historyOK:    true,
		requirements: g.requirements,
	}

	intents, signals, cbErr := g.strat.OnBar(g.ctx, event, view)
	if cbErr != nil {
		return g.sendOnBarResponse(w.GetSequence(), nil, nil, toWireError(v1.ErrorCode_ERROR_CODE_CALLBACK_FAILED, cbErr))
	}

	wireIntents := make([]*v1.DescribedIntent, len(intents))
	for i, in := range intents {
		wi, err := toWireDescribedIntent(in)
		if err != nil {
			return g.sendOnBarResponse(w.GetSequence(), nil, nil, toWireError(v1.ErrorCode_ERROR_CODE_INVALID_DESCRIBED_INTENT, err))
		}
		wireIntents[i] = wi
	}
	wireSignals := make([]*v1.DescribedSignal, len(signals))
	for i, sig := range signals {
		wireSignals[i] = toWireDescribedSignal(sig)
	}

	return g.sendOnBarResponse(w.GetSequence(), wireIntents, wireSignals, nil)
}

func (g *guestRun) sendOnBarResponse(sequence uint64, intents []*v1.DescribedIntent, signals []*v1.DescribedSignal, wireErr *v1.Error) error {
	return g.stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnBarResponse{OnBarResponse: &v1.OnBarResponse{
		Sequence: sequence,
		Intents:  intents,
		Signals:  signals,
		Error:    wireErr,
	}}})
}

func (g *guestRun) handleFillEvent(w *v1.FillEvent) error {
	if w == nil {
		return fmt.Errorf("%w: fill_event must be set", ErrInvalidWireValue)
	}
	event, err := fromWireFillEvent(w)
	if err != nil {
		return fmt.Errorf("strategysdk: fill event: %w", err)
	}
	account, err := fromWireAccountSnapshot(w.GetAccount())
	if err != nil {
		return fmt.Errorf("strategysdk: fill event: %w", err)
	}

	// GetHistoryBars is scoped to an in-flight BarEvent callback only
	// (strategy.proto's own GetHistoryBarsRequest doc comment); a
	// fill callback's own View never exposes it, matching
	// ExternalStrategyAdapter's own OnFill (adapters/strategy/external's
	// own core.go passes nil view to awaitResponse for exactly this
	// reason).
	view := &guestView{account: account, historyOK: false}

	var wireErr *v1.Error
	if g.fillHandler != nil {
		if cbErr := g.fillHandler.OnFill(g.ctx, event, view); cbErr != nil {
			wireErr = toWireError(v1.ErrorCode_ERROR_CODE_CALLBACK_FAILED, cbErr)
		}
	} else {
		wireErr = toWireError(v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH,
			fmt.Errorf("strategysdk: received fill_event but this strategy does not implement FillHandler"))
	}

	return g.stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnFillResponse{OnFillResponse: &v1.OnFillResponse{
		Sequence: w.GetSequence(),
		Error:    wireErr,
	}}})
}

// sessionEndErr converts a terminal SessionEnd into a Go error, nil
// for the documented normal-completion zero value.
func sessionEndErr(w *v1.SessionEnd) error {
	if w == nil {
		return nil
	}
	if w.GetCode() == v1.ErrorCode_ERROR_CODE_UNSPECIFIED && w.GetReason() == "" {
		return nil
	}
	return &WireError{Code: w.GetCode(), Message: w.GetReason()}
}

// guestView implements View for one in-flight callback.
type guestView struct {
	ctx       context.Context
	client    v1.StrategyHostServiceClient
	sessionID string
	sequence  uint64
	account   AccountSnapshot

	historyOK    bool
	requirements []DataRequirement
}

func (v *guestView) Account() AccountSnapshot { return v.account }

func (v *guestView) HistoryBars(instID instrument.ID, interval marketdata.Interval, n int) ([]marketdata.Bar, bool) {
	if !v.historyOK || n <= 0 {
		return nil, false
	}

	declared := false
	for _, r := range v.requirements {
		if r.Instrument.Equal(instID) && r.Interval == interval {
			declared = true
			break
		}
	}
	if !declared {
		return nil, false
	}

	wireIv, err := toWireInterval(interval)
	if err != nil {
		return nil, false
	}

	resp, err := v.client.GetHistoryBars(v.ctx, &v1.GetHistoryBarsRequest{
		SessionId:        v.sessionID,
		CallbackSequence: v.sequence,
		InstrumentId:     instID.String(),
		Interval:         wireIv,
		Count:            int32(n),
	})
	if err != nil {
		return nil, false
	}

	bars, err := fromWireBars(resp.GetBars())
	if err != nil {
		return nil, false
	}
	return bars, true
}
