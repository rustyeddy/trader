package external

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
	var fill, bars bool
	for _, c := range session.capabilities {
		switch c {
		case v1.Capability_CAPABILITY_FILL_HANDLER:
			fill = true
		case v1.Capability_CAPABILITY_BARS_DELIVERY:
			bars = true
		}
	}
	switch {
	case bars && fill:
		return &externalBarsWithFill{externalBars: externalBars{externalStrategyCore: core}}
	case bars:
		return &externalBars{externalStrategyCore: core}
	case fill:
		return &externalStrategyWithFill{externalStrategyCore: core}
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

// externalBars is the strategy.Strategy returned when the guest
// negotiated CAPABILITY_BARS_DELIVERY (issue #467): it satisfies
// strategy.BarsHandler, so a runtime delivers one OnBars snapshot per
// completed boundary and never calls OnBar. OnBar exists only to
// satisfy the strategy.Strategy interface; it deliberately fails rather
// than forward a single bar, since the guest declared snapshot delivery
// and has no OnBar. The plain and fill wrappers above intentionally do
// not have OnBars, so exactly one delivery shape is ever visible to a
// runtime.
type externalBars struct {
	*externalStrategyCore
}

// externalBarsWithFill additionally satisfies strategy.FillHandler.
type externalBarsWithFill struct {
	externalBars
}

var (
	_ strategy.Strategy    = (*externalStrategyPlain)(nil)
	_ strategy.Strategy    = (*externalStrategyWithFill)(nil)
	_ strategy.FillHandler = (*externalStrategyWithFill)(nil)
	_ strategy.Strategy    = (*externalBars)(nil)
	_ strategy.BarsHandler = (*externalBars)(nil)
	_ strategy.Strategy    = (*externalBarsWithFill)(nil)
	_ strategy.BarsHandler = (*externalBarsWithFill)(nil)
	_ strategy.FillHandler = (*externalBarsWithFill)(nil)
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

	intents, err := c.translate(ctx, event.Bar.Time, onBarResp.GetIntents(), onBarResp.GetSignals())
	if err != nil {
		return nil, fmt.Errorf("external: on bar: %w", err)
	}
	return intents, nil
}

// translate turns one callback response's described intents into
// canonical intents and journals its described signals, stamped at ts —
// the ownership rule shared by OnBar and OnBars: the guest describes,
// the host mints.
func (c *externalStrategyCore) translate(ctx context.Context, ts time.Time, described []*v1.DescribedIntent, signals []*v1.DescribedSignal) ([]runtimeorder.Intent, error) {
	intents, tokens, err := IntentsFromWire(described, c.factory)
	if err != nil {
		return nil, err
	}
	if err := c.recordSignals(ctx, ts, signals, tokens); err != nil {
		return nil, err
	}
	return intents, nil
}

// OnBar on a snapshot-delivery adapter is a contract violation: the
// guest negotiated CAPABILITY_BARS_DELIVERY and cannot handle single
// bars. A runtime must honor strategy.BarsHandler instead.
func (b *externalBars) OnBar(context.Context, strategy.BarEvent, strategy.View) ([]runtimeorder.Intent, error) {
	return nil, fmt.Errorf("external: on bar: this session negotiated bars delivery; the runtime must call OnBars")
}

// OnBars writes one BarsEvent snapshot down the Run stream and blocks
// for the guest's correlated OnBarsResponse, with exactly OnBar's
// intent, signal and failure semantics. The frozen view is registered
// for GetHistoryBars the same way OnBar's is, so history during OnBars
// is strictly before the snapshot boundary.
func (b *externalBars) OnBars(ctx context.Context, event strategy.BarsEvent, view strategy.View) ([]runtimeorder.Intent, error) {
	sequence := b.session.nextSequence()
	wireEvent, err := ToWireBarsEvent(sequence, event, view.Account())
	if err != nil {
		return nil, fmt.Errorf("external: on bars: %w", err)
	}
	msg := &v1.RunServerMessage{Payload: &v1.RunServerMessage_BarsEvent{BarsEvent: wireEvent}}

	resp, err := b.session.submit(ctx, sequence, msg, view)
	if err != nil {
		return nil, fmt.Errorf("external: on bars: %w", err)
	}
	onBarsResp := resp.GetOnBarsResponse()
	if onBarsResp == nil {
		return nil, fmt.Errorf("external: on bars: expected on_bars_response, got %T", resp.GetPayload())
	}
	if cbErr := FromWireError(onBarsResp.GetError()); cbErr != nil {
		return nil, fmt.Errorf("external: on bars: %w", cbErr)
	}
	intents, err := b.translate(ctx, event.Boundary, onBarsResp.GetIntents(), onBarsResp.GetSignals())
	if err != nil {
		return nil, fmt.Errorf("external: on bars: %w", err)
	}
	return intents, nil
}

// recordSignals journals every described signal from one OnBarResponse,
// if a Journal was configured — mirroring strategy/smatrend's own
// recordSignal (ADR-044), except the evidence and correlation
// resolution both cross the process boundary via SignalFromWire.
func (c *externalStrategyCore) recordSignals(ctx context.Context, ts time.Time, signals []*v1.DescribedSignal, tokens map[string]id.CorrelationID) error {
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
			Metadata: id.Metadata{CorrelationID: corr, Timestamp: ts},
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
	return c.onFill(ctx, event, view)
}

// OnFill for bars-delivery sessions that also negotiated
// CAPABILITY_FILL_HANDLER.
func (b *externalBarsWithFill) OnFill(ctx context.Context, event strategy.FillEvent, view strategy.View) error {
	return b.onFill(ctx, event, view)
}

func (c *externalStrategyCore) onFill(ctx context.Context, event strategy.FillEvent, view strategy.View) error {
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
