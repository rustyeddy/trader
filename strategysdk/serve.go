package strategysdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
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

// DefaultHandshakeTimeout bounds the Handshake RPC (and, since
// grpc.NewClient dials lazily, the effective connection attempt to
// the Unix-domain socket along with it). ADR-062 assigns ownership of
// every unary RPC's own deadline to the guest/SDK, since the guest is
// always the gRPC client for Handshake and GetHistoryBars; without a
// bound here, a missing or unresponsive host would leave Serve
// blocked indefinitely rather than failing with a clear error. See
// WithHandshakeTimeout.
const DefaultHandshakeTimeout = 10 * time.Second

// DefaultHistoryBarsTimeout bounds each GetHistoryBars RPC — the
// same unary-RPC-deadline ownership DefaultHandshakeTimeout documents,
// applied to the one other unary RPC the guest itself initiates. See
// WithHistoryBarsTimeout.
const DefaultHistoryBarsTimeout = 5 * time.Second

// Option configures Serve, ServeContext, or ServeConn. See WithLogger,
// WithHandshakeTimeout, and WithHistoryBarsTimeout.
type Option func(*runConfig)

type runConfig struct {
	logger             *slog.Logger
	handshakeTimeout   time.Duration
	historyBarsTimeout time.Duration
}

func defaultRunConfig() runConfig {
	return runConfig{
		logger:             slog.Default(),
		handshakeTimeout:   DefaultHandshakeTimeout,
		historyBarsTimeout: DefaultHistoryBarsTimeout,
	}
}

// WithLogger overrides the *slog.Logger a strategy's own Environment
// carries, and that Serve/ServeContext/ServeConn use for their own
// lifecycle logging. The default is slog.Default() — this process's
// own configured default logger (stderr, by the standard library's
// own default, matching Trader's own "logger output defaults to
// stderr" convention) — never a silently discarding one, so a real
// guest process's own diagnostics are visible unless the caller
// explicitly chooses otherwise.
func WithLogger(l *slog.Logger) Option {
	return func(c *runConfig) {
		// A nil l must not overwrite the non-nil default: Environment's
		// own doc comment promises Logger is never nil, and this option
		// applies after defaultRunConfig has already populated a real
		// logger — silently accepting nil here would break that promise
		// for any caller that passes a nil *slog.Logger, intentionally
		// or not (review finding).
		if l != nil {
			c.logger = l
		}
	}
}

// WithHandshakeTimeout overrides DefaultHandshakeTimeout.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(c *runConfig) { c.handshakeTimeout = d }
}

// WithHistoryBarsTimeout overrides DefaultHistoryBarsTimeout.
func WithHistoryBarsTimeout(d time.Duration) Option {
	return func(c *runConfig) { c.historyBarsTimeout = d }
}

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
func Serve(strat Strategy, opts ...Option) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return ServeContext(ctx, strat, opts...)
}

// ServeContext is Serve without built-in OS signal handling: it reads
// SocketPathEnv, dials, and drives strat until the host ends the
// session, the connection fails, or ctx is done.
func ServeContext(ctx context.Context, strat Strategy, opts ...Option) error {
	sockPath := os.Getenv(SocketPathEnv)
	if sockPath == "" {
		return fmt.Errorf("strategysdk: %s is not set — this process must be launched by a Trader host (ADR-063)", SocketPathEnv)
	}

	conn, err := grpc.NewClient("unix:"+sockPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("strategysdk: dialing %s: %w", sockPath, err)
	}
	defer func() { _ = conn.Close() }()

	return ServeConn(ctx, conn, strat, opts...)
}

// ServeConn drives strat through Strategy Protocol v1 over an
// already-established gRPC connection — the lower-level entry point
// ServeContext itself uses after dialing, exposed directly so a
// caller (typically a test) can supply its own grpc.ClientConnInterface,
// for example one backed by an in-process bufconn listener, without
// needing a real Unix-domain socket.
func ServeConn(ctx context.Context, conn grpc.ClientConnInterface, strat Strategy, opts ...Option) error {
	cfg := defaultRunConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	client := v1.NewStrategyHostServiceClient(conn)

	capabilities := negotiatedCapabilities(strat)

	// Describe() is called exactly once and its result reused for both
	// the wire Handshake and guestRun's own local requirements set.
	// ADR-062 defines the Handshake descriptor as the single
	// declaration used for all later GetHistoryBars scoping; calling
	// Describe() a second time to build guestRun.requirements let a
	// stateful, time-sensitive, or simply buggy implementation diverge
	// from what the host actually accepted — locally authorizing a
	// requirement the host never saw, or rejecting one it did (review
	// finding).
	descriptor := strat.Describe()
	wireDescriptor, err := toWireDescriptor(descriptor)
	if err != nil {
		return fmt.Errorf("strategysdk: describe: %w", err)
	}

	handshakeCtx, cancelHandshake := context.WithTimeout(ctx, cfg.handshakeTimeout)
	hsResp, err := client.Handshake(handshakeCtx, &v1.HandshakeRequest{
		ProtocolVersion:    v1.ProtocolVersion,
		StrategyDescriptor: wireDescriptor,
		Capabilities:       capabilities,
	})
	cancelHandshake()
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
	if err := verifyNegotiatedCapabilities(capabilities, hsResp.GetCapabilities()); err != nil {
		return fmt.Errorf("strategysdk: handshake: %w", err)
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
		// stream.Context(), not the outer ctx: once the Run RPC itself
		// ends (for any reason — the host's own callback-timeout
		// teardown included, ADR-062's own supervision policy), this
		// context is canceled too. A callback or GetHistoryBars call
		// still keyed to the outer ctx could otherwise remain blocked
		// after the host has already given up on this session (review
		// finding). The outer ctx still governs Handshake, above,
		// since no stream exists yet at that point.
		ctx:                stream.Context(),
		client:             client,
		stream:             stream,
		sessionID:          hsResp.GetSessionId(),
		strat:              strat,
		fillHandler:        asFillHandler(strat),
		requirements:       descriptor.Requirements,
		logger:             cfg.logger,
		historyBarsTimeout: cfg.historyBarsTimeout,
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

// verifyNegotiatedCapabilities fails explicitly if the host's own
// accepted response drops a capability this guest requested — ADR-062
// defines negotiation as bilateral, and a host that accepts the
// connection but silently declines a capability this runtime actually
// depends on (currently: FillHandler, when strat implements it) must
// not be allowed to run with that capability quietly disabled (review
// finding): a guest that implements FillHandler but never receives
// fills is a silent behavior change, not a graceful degradation.
func verifyNegotiatedCapabilities(requested, accepted []v1.Capability) error {
	acceptedSet := make(map[v1.Capability]bool, len(accepted))
	for _, c := range accepted {
		acceptedSet[c] = true
	}
	for _, c := range requested {
		if !acceptedSet[c] {
			return fmt.Errorf("%w: host did not negotiate capability %s this strategy requires", ErrInvalidWireValue, c)
		}
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
	ctx                context.Context // stream.Context(), not ServeConn's own outer ctx — see ServeConn's own comment
	client             v1.StrategyHostServiceClient
	stream             v1.StrategyHostService_RunClient
	sessionID          string
	strat              Strategy
	fillHandler        FillHandler // nil unless strat implements FillHandler
	requirements       []DataRequirement
	logger             *slog.Logger
	historyBarsTimeout time.Duration

	clock         *hostClock // set once SessionStart arrives
	runID         string
	sawSessionEnd bool
}

// loop drives the Run stream until the host sends a terminal
// SessionEnd, the stream itself ends abnormally, or ctx is done.
//
// A bare io.EOF without ever having observed SessionEnd is treated as
// abnormal termination, not a clean exit (review finding): v1's own
// documented normal-completion signal is SessionEnd; a stream that
// simply disappears (a crashed host, a dropped connection) must not
// be indistinguishable from one the host ended on purpose, or a
// caller could report success after silently losing the session.
func (g *guestRun) loop() error {
	for {
		msg, err := g.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if g.sawSessionEnd {
					return nil
				}
				return fmt.Errorf("strategysdk: run stream: ended without a session_end message (host or transport failure)")
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
			g.sawSessionEnd = true
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
		Logger: g.logger,
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
		ctx:                g.ctx,
		client:             g.client,
		sessionID:          g.sessionID,
		sequence:           w.GetSequence(),
		account:            account,
		historyOK:          true,
		requirements:       g.requirements,
		historyBarsTimeout: g.historyBarsTimeout,
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
	// reason). See View.HistoryBars' own doc comment for exactly what
	// this View reports when called during OnFill.
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

	historyOK          bool
	requirements       []DataRequirement
	historyBarsTimeout time.Duration
}

func (v *guestView) Account() AccountSnapshot { return v.account }

// HistoryBars returns up to n of the most-recently-closed bars for
// (instID, interval), oldest-first, by calling the real GetHistoryBars
// RPC (bounded by historyBarsTimeout — ADR-062's own guest-owned
// unary-RPC-deadline rule).
//
// Unlike strategy.History.HistoryBars' own two-result contract, this
// method has a third, explicit error result: ok == false alone means
// exactly one thing — (instID, interval) was not declared as one of
// this strategy's own DataRequirements, or this View was built for an
// OnFill callback, which v1 never scopes history to at all (both
// cases the host itself would also refuse). A transport failure,
// RPC deadline, or malformed response is a distinct, non-nil err
// instead of being folded into ok == false (review finding: an
// earlier version could not tell a dead host apart from an
// undeclared requirement, both surfacing identically as "no
// history" to strategy code). n <= 0 is not an error: it returns an
// empty, non-nil slice with ok == true when the requirement is
// otherwise declared, mirroring the in-process backtest.Scheduler's
// own History implementation exactly.
func (v *guestView) HistoryBars(instID instrument.ID, interval marketdata.Interval, n int) (bars []marketdata.Bar, ok bool, err error) {
	if !v.historyOK {
		return nil, false, nil
	}

	declared := false
	for _, r := range v.requirements {
		if r.Instrument.Equal(instID) && r.Interval == interval {
			declared = true
			break
		}
	}
	if !declared {
		return nil, false, nil
	}
	if n <= 0 {
		return []marketdata.Bar{}, true, nil
	}
	if n > math.MaxInt32 {
		return nil, false, fmt.Errorf("%w: history bars: count %d exceeds int32 range", ErrInvalidWireValue, n)
	}

	wireIv, err := toWireInterval(interval)
	if err != nil {
		return nil, false, fmt.Errorf("strategysdk: history bars: %w", err)
	}

	rpcCtx, cancel := context.WithTimeout(v.ctx, v.historyBarsTimeout)
	defer cancel()

	resp, err := v.client.GetHistoryBars(rpcCtx, &v1.GetHistoryBarsRequest{
		SessionId:        v.sessionID,
		CallbackSequence: v.sequence,
		InstrumentId:     instID.String(),
		Interval:         wireIv,
		Count:            int32(n),
	})
	if err != nil {
		return nil, false, fmt.Errorf("strategysdk: history bars: %w", err)
	}

	bars, err = fromWireBars(resp.GetBars())
	if err != nil {
		return nil, false, fmt.Errorf("strategysdk: history bars: %w", err)
	}
	return bars, true, nil
}
