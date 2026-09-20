package external

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/journal"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/strategy"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// externalStrategyCore holds the shared Run-stream/state-machine
// logic every capability combination needs (ADR-062's own "Optional
// capabilities cannot vary at runtime on one concrete Go type"
// section): Go method sets are static, so a single concrete type
// cannot conditionally satisfy strategy.FillHandler based on what one
// particular guest's own Handshake declared. newExternalStrategyAdapter
// (below) picks the one wrapper type matching the negotiated
// capability set instead.
type externalStrategyCore struct {
	session *runSession
	logger  *slog.Logger

	// Set by Start, from its own Environment — never constructed by
	// this package itself (ADR-005/ADR-062's own "intent construction
	// ownership": an external strategy process never mints a real
	// order.IntentID/id.EventID/id.CorrelationID, and neither does the
	// adapter translating for it).
	factory strategy.IntentFactory
	runID   id.RunID
	journal journal.Recorder // nil unless env.Journal was set
}

// newExternalStrategyAdapter returns the strategy.Strategy wrapper
// matching session's own negotiated capabilities: with
// CAPABILITY_FILL_HANDLER, the returned value also satisfies
// strategy.FillHandler. Both wrapper types additionally expose Close
// (below), a plain method — the strategy package defines no Closer
// capability interface today, but a composition root that knows it is
// driving an external strategy can still call it explicitly to send a
// clean SessionEnd before tearing down the session.
func newExternalStrategyAdapter(session *runSession, logger *slog.Logger) strategy.Strategy {
	core := &externalStrategyCore{session: session, logger: logger}
	for _, c := range session.capabilities {
		if c == v1.Capability_CAPABILITY_FILL_HANDLER {
			return &externalStrategyWithFill{externalStrategyCore: core}
		}
	}
	return &externalStrategyPlain{externalStrategyCore: core}
}

// externalStrategyPlain is the strategy.Strategy returned when the
// guest negotiated no optional capabilities at Handshake.
type externalStrategyPlain struct {
	*externalStrategyCore
}

// externalStrategyWithFill additionally satisfies strategy.FillHandler
// when the guest negotiated CAPABILITY_FILL_HANDLER (ADR-060).
type externalStrategyWithFill struct {
	*externalStrategyCore
}

var (
	_ strategy.Strategy    = (*externalStrategyPlain)(nil)
	_ strategy.Strategy    = (*externalStrategyWithFill)(nil)
	_ strategy.FillHandler = (*externalStrategyWithFill)(nil)
)

// Describe answers from the StrategyDescriptor the guest's own
// Handshake already carried — no further RPC round trip (ADR-062).
func (c *externalStrategyCore) Describe() strategy.Descriptor {
	return c.session.descriptor
}

// Start retains env.Intents/env.RunID/env.Journal for the session's
// lifetime and sends SessionStart down the Run stream the instant
// they become known — waiting first, if necessary, for the guest's
// own RunOpen to have bound that stream.
func (c *externalStrategyCore) Start(ctx context.Context, env strategy.Environment) error {
	if err := c.session.waitStream(ctx); err != nil {
		return fmt.Errorf("external: start: %w", err)
	}

	// Mirrors strategy/smatrend's own Start-time check: a Journal
	// without a real RunID could never actually build a valid
	// journal.Record later (journal.NewRecord requires RunID), so this
	// fails now rather than deferring the failure until the first
	// signal arrives (review finding).
	if env.Journal != nil && env.RunID.IsZero() {
		return fmt.Errorf("external: start: env.Journal is set but env.RunID is zero: every recorded signal needs a run id")
	}

	c.factory = env.Intents
	c.runID = env.RunID
	c.journal = env.Journal
	if env.Logger != nil {
		c.logger = env.Logger
	}

	msg := &v1.RunServerMessage{Payload: &v1.RunServerMessage_SessionStart{
		SessionStart: ToWireSessionStart(env.RunID, env.Clock.Now()),
	}}
	if err := c.session.sysSend(ctx, msg); err != nil {
		return fmt.Errorf("external: start: sending session_start: %w", err)
	}
	return nil
}

// OnBar writes event down the Run stream and blocks for the guest's
// own correlated OnBarResponse, translating its described intents
// into canonical order.Intent values through the retained
// strategy.IntentFactory and journaling any described signals.
func (c *externalStrategyCore) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]runtimeorder.Intent, error) {
	sequence := c.session.nextSequence()
	wireEvent, err := ToWireBarEvent(sequence, event, view.Account())
	if err != nil {
		return nil, fmt.Errorf("external: on bar: %w", err)
	}
	msg := &v1.RunServerMessage{Payload: &v1.RunServerMessage_BarEvent{BarEvent: wireEvent}}

	resp, err := c.session.submit(ctx, sequence, msg, view)
	if err != nil {
		return nil, fmt.Errorf("external: on bar: %w", err)
	}

	onBarResp := resp.GetOnBarResponse()
	if onBarResp == nil {
		return nil, fmt.Errorf("external: on bar: expected on_bar_response, got %T", resp.GetPayload())
	}
	if cbErr := FromWireError(onBarResp.GetError()); cbErr != nil {
		return nil, fmt.Errorf("external: on bar: %w", cbErr)
	}

	intents, tokens, err := IntentsFromWire(onBarResp.GetIntents(), c.factory)
	if err != nil {
		return nil, fmt.Errorf("external: on bar: %w", err)
	}

	if err := c.recordSignals(ctx, event, onBarResp.GetSignals(), tokens); err != nil {
		return nil, fmt.Errorf("external: on bar: %w", err)
	}

	return intents, nil
}

// recordSignals journals every described signal from one OnBarResponse,
// if a Journal was configured — mirroring strategy/smatrend's own
// recordSignal (ADR-044), except the evidence and correlation
// resolution both cross the process boundary via SignalFromWire.
func (c *externalStrategyCore) recordSignals(ctx context.Context, event strategy.BarEvent, signals []*v1.DescribedSignal, tokens map[string]id.CorrelationID) error {
	if c.journal == nil || len(signals) == 0 {
		return nil
	}
	for i, wireSig := range signals {
		sig, corr, err := SignalFromWire(wireSig, tokens)
		if err != nil {
			return fmt.Errorf("signal %d: %w", i, err)
		}
		rec, err := journal.NewRecord(journal.Record{
			RunID:    c.runID,
			Metadata: id.Metadata{CorrelationID: corr, Timestamp: event.Bar.Time},
			Kind:     journal.KindSignal,
			Signal:   &sig,
		})
		if err != nil {
			return fmt.Errorf("signal %d: %w", i, err)
		}
		if err := c.journal.Record(ctx, rec); err != nil {
			return fmt.Errorf("signal %d: %w", i, err)
		}
	}
	return nil
}

// Close sends a normal-completion SessionEnd down the Run stream —
// the Run stream's own documented terminal server->client message —
// and then ends the Run RPC itself (review finding: sending
// SessionEnd alone left the Run handler still receiving, so a guest
// that kept its stream open could remain "active" after Close). It is
// not part of the strategy.Strategy contract; see newExternalStrategy
// Adapter's own doc comment for why it is exposed anyway.
func (c *externalStrategyCore) Close(ctx context.Context) error {
	end, err := ToWireSessionEnd(v1.ErrorCode_ERROR_CODE_UNSPECIFIED, nil)
	if err != nil {
		return fmt.Errorf("external: close: %w", err)
	}
	msg := &v1.RunServerMessage{Payload: &v1.RunServerMessage_SessionEnd{SessionEnd: end}}
	if err := c.session.sysSend(ctx, msg); err != nil {
		return fmt.Errorf("external: close: sending session_end: %w", err)
	}
	c.session.forceTeardown(nil) // nil: deliberate, graceful end — Run returns nil, not an error status
	return nil
}

// OnFill writes event down the Run stream and blocks for the guest's
// own correlated OnFillResponse. Only externalStrategyWithFill exposes
// this — CAPABILITY_FILL_HANDLER was negotiated at Handshake (ADR-060).
//
// view is deliberately not exposed to a concurrent GetHistoryBars
// call (submit's own nil-view parameter, below): strategy.proto's
// own GetHistoryBarsRequest doc comment scopes callback_sequence to an
// in-flight BarEvent/OnBarResponse callback only, never a fill
// callback (review finding).
func (c *externalStrategyWithFill) OnFill(ctx context.Context, event strategy.FillEvent, view strategy.View) error {
	sequence := c.session.nextSequence()
	wireEvent, err := ToWireFillEvent(sequence, event, view.Account())
	if err != nil {
		return fmt.Errorf("external: on fill: %w", err)
	}
	msg := &v1.RunServerMessage{Payload: &v1.RunServerMessage_FillEvent{FillEvent: wireEvent}}

	resp, err := c.session.submit(ctx, sequence, msg, nil)
	if err != nil {
		return fmt.Errorf("external: on fill: %w", err)
	}

	onFillResp := resp.GetOnFillResponse()
	if onFillResp == nil {
		return fmt.Errorf("external: on fill: expected on_fill_response, got %T", resp.GetPayload())
	}
	if cbErr := FromWireError(onFillResp.GetError()); cbErr != nil {
		return fmt.Errorf("external: on fill: %w", cbErr)
	}
	return nil
}
