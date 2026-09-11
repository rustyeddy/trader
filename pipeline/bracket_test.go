package pipeline_test

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustEnterWithStopIntent builds a valid order.IntentEnterWithStop
// (issue #351, ADR-059) for side, protected by stopPrice — a level
// computed independently of any real fill price, exactly as this
// intent's own contract requires.
func mustEnterWithStopIntent(t *testing.T, ids *id.Generator, instID instrument.ID, side order.Side, stopPrice string) order.Intent {
	t.Helper()
	intentID, err := id.GenerateIntentID(ids)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(ids)
	require.NoError(t, err)
	corrID, err := id.GenerateCorrelationID(ids)
	require.NoError(t, err)
	sp := num.MustParsePrice(stopPrice)
	in, err := order.NewIntent(order.Intent{
		IntentID:   intentID,
		Kind:       order.IntentEnterWithStop,
		Instrument: instID,
		Side:       side,
		StopPrice:  &sp,
		Metadata:   id.Metadata{EventID: eventID, CorrelationID: corrID},
	})
	require.NoError(t, err)
	return in
}

// TestPipelineSubmit_BracketEndToEndAgainstSimulator proves the core
// claim issue #351/ADR-059 makes: against the simulator (which fills
// a Market order synchronously, inside Submit itself), a bracket
// intent leaves the account with both a filled Long position *and* an
// already-resting protective Stop order in one Submit call — no bar
// of unprotected exposure between the two.
func TestPipelineSubmit_BracketEndToEndAgainstSimulator(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)

	snap := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterWithStopIntent(t, h.ids, h.listing.InstrumentID(), order.Buy, "1.05000")

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         snap,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Bracket)

	// Entry leg: an ordinary, filled IntentEnter result.
	entry := result.Bracket.Entry
	assert.True(t, entry.Decision.Allowed)
	assert.Equal(t, order.Buy, entry.Proposal.Side)
	assert.Equal(t, order.Market, entry.Proposal.Type)
	assert.Equal(t, order.StatusFilled, entry.Order.Status)

	// Stop leg: an ordinary, working (resting) IntentAdjustStop
	// result, planned and risk-evaluated against the account state
	// *after* the entry's own fill.
	stop := result.Bracket.Stop
	assert.True(t, stop.Decision.Allowed)
	assert.Equal(t, order.Stop, stop.Proposal.Type)
	assert.True(t, stop.Proposal.ReduceOnly)
	require.NotNil(t, stop.Proposal.StopPrice)
	assert.Equal(t, "1.05", stop.Proposal.StopPrice.String())
	assert.Equal(t, order.StatusWorking, stop.Order.Status)

	// Both legs correlate to the bracket intent's own CorrelationID
	// (the shared workflow every stage of both legs traces back to).
	// Each leg's own CausationID is its own freshly-generated
	// sub-intent's EventID (itself caused by the bracket's own
	// EventID, one hop further back — see buildBracketSubIntent's own
	// doc comment) rather than the bracket's EventID directly, so the
	// two legs' own CausationIDs are real, independently-generated
	// values, distinct from each other and from the bracket's own
	// EventID.
	assert.Equal(t, intent.Metadata.CorrelationID, entry.Proposal.Metadata.CorrelationID)
	assert.Equal(t, intent.Metadata.CorrelationID, stop.Proposal.Metadata.CorrelationID)
	assert.NotEqual(t, intent.Metadata.EventID, entry.Proposal.Metadata.CausationID)
	assert.NotEqual(t, intent.Metadata.EventID, stop.Proposal.Metadata.CausationID)
	assert.NotEqual(t, entry.Proposal.Metadata.CausationID, stop.Proposal.Metadata.CausationID)

	// Real broker state: one Long position, one resting Stop order —
	// both visible in the same post-Submit snapshot, confirming this
	// is genuinely one atomic-from-the-caller's-perspective outcome,
	// not merely two Results that happen to be bundled.
	after := h.snapshot(t, ctx)
	require.Len(t, after.Positions(), 1)
	assert.Equal(t, order.Long, after.Positions()[0].Side)
	require.Len(t, after.OpenOrders(), 1)
	assert.Equal(t, order.Stop, after.OpenOrders()[0].Request.Type)
	assert.Equal(t, order.StatusWorking, after.OpenOrders()[0].Status)
}

// TestPipelineEvaluate_RejectsBracketIntent proves Evaluate never
// attempts to preview an order.IntentEnterWithStop (ADR-059's own
// documented reason: the stop leg's viability cannot be known without
// a real broker call for the entry leg first) — it returns
// ErrBracketRequiresSubmit immediately, with the broker never touched.
func TestPipelineEvaluate_RejectsBracketIntent(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)

	snap := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterWithStopIntent(t, h.ids, h.listing.InstrumentID(), order.Buy, "1.05000")

	result, err := p.Evaluate(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         snap,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, pipeline.ErrBracketRequiresSubmit)
	assert.Equal(t, pipeline.Result{}, result)

	after := h.snapshot(t, ctx)
	assert.Empty(t, after.Positions(), "Evaluate must never touch the broker for a bracket intent")
}

// TestPipelineSubmit_BracketEntryRejectedNeverAttemptsStop proves a
// risk-rejected entry leg stops the bracket immediately: the stop leg
// is never planned, risk-evaluated, or submitted, and the account is
// left exactly as it started.
func TestPipelineSubmit_BracketEntryRejectedNeverAttemptsStop(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h, rejectingRule{})

	before := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterWithStopIntent(t, h.ids, h.listing.InstrumentID(), order.Buy, "1.05000")

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         before,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, pipeline.ErrRejected)
	require.NotNil(t, result.Bracket)
	assert.False(t, result.Bracket.Entry.Decision.Allowed)
	assert.Equal(t, pipeline.Result{}, result.Bracket.Stop, "the stop leg must never be attempted when the entry leg is rejected")

	after := h.snapshot(t, ctx)
	assert.Empty(t, after.Positions())
	assert.Empty(t, after.OpenOrders())
}

// stopRejectingRule rejects only Stop-type proposals, letting a
// Market entry through — used to prove the bracket's own entry can
// genuinely fill while its stop leg is separately, independently
// risk-evaluated and can be rejected on its own merits.
type stopRejectingRule struct{}

func (stopRejectingRule) Name() string { return "reject_stops" }
func (stopRejectingRule) Evaluate(ctx context.Context, in risk.Input) (risk.RuleResult, error) {
	if in.Proposal.Type == order.Stop {
		return risk.RuleResult{Violations: []risk.Violation{{Message: "test: stop orders always rejected"}}}, nil
	}
	return risk.RuleResult{}, nil
}

// TestPipelineSubmit_BracketStopRejectedLeavesEntryFilledAndUnprotected
// proves the entry leg's own success is not undone by a stop-leg
// rejection: the position stays open (the broker was already told to
// buy; nothing rolls that back), while the caller sees a real error
// and can decide how to protect the now-open, unprotected position —
// Pipeline itself makes no such decision on the caller's behalf (see
// ErrBracketEntryNotSynchronouslyFilled's own doc comment for the
// identical principle applied to the other way a bracket's stop leg
// can fail to attach).
func TestPipelineSubmit_BracketStopRejectedLeavesEntryFilledAndUnprotected(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h, stopRejectingRule{})

	before := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterWithStopIntent(t, h.ids, h.listing.InstrumentID(), order.Buy, "1.05000")

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         before,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, pipeline.ErrRejected)
	require.NotNil(t, result.Bracket)
	assert.True(t, result.Bracket.Entry.Decision.Allowed)
	assert.Equal(t, order.StatusFilled, result.Bracket.Entry.Order.Status)
	assert.False(t, result.Bracket.Stop.Decision.Allowed)

	after := h.snapshot(t, ctx)
	require.Len(t, after.Positions(), 1, "the entry leg's own fill is real broker state and is not rolled back by the stop leg's own rejection")
	assert.Empty(t, after.OpenOrders(), "no resting stop exists: the caller must protect this position through some other means")
}

// notSynchronouslyFillingBroker wraps a real sim.Broker's own
// OpenAccount result so Submit accepts an order (StatusWorking, never
// StatusFilled) without ever actually filling it — modeling a real
// broker's own asynchronous fill reporting (ADR-059's own explicitly
// out-of-scope case), unlike the simulator's real synchronous Market
// fill.
type notSynchronouslyFillingBroker struct {
	inner brokerpkg.Broker
}

func (b notSynchronouslyFillingBroker) Name() string { return b.inner.Name() }

func (b notSynchronouslyFillingBroker) Accounts(ctx context.Context) ([]account.Reference, error) {
	return b.inner.Accounts(ctx)
}

func (b notSynchronouslyFillingBroker) OpenAccount(ctx context.Context, accountID id.AccountID) (brokerpkg.Account, error) {
	acc, err := b.inner.OpenAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return notSynchronouslyFillingAccount{inner: acc}, nil
}

func (b notSynchronouslyFillingBroker) Close() error { return b.inner.Close() }

type notSynchronouslyFillingAccount struct {
	inner brokerpkg.Account
}

func (a notSynchronouslyFillingAccount) Reference() account.Reference { return a.inner.Reference() }

func (a notSynchronouslyFillingAccount) Snapshot(ctx context.Context) (account.Snapshot, error) {
	return a.inner.Snapshot(ctx)
}

// Submit reports StatusWorking regardless of what the real broker
// underneath actually did — this test double exists specifically to
// simulate a broker whose own Submit return value does not yet
// reflect a fill, per broker.Account.Submit's own doc comment
// ("returning the resulting Order in whatever state the broker
// reports synchronously — for example StatusPendingSubmit or
// StatusWorking"). It never actually calls the inner broker's Submit
// at all: a bracket entry that submitBracket believes is still
// working must never proceed to the stop leg, so there is nothing for
// this double to fill even if it wanted to.
func (a notSynchronouslyFillingAccount) Submit(ctx context.Context, req order.Request) (order.Order, error) {
	accepted := req.Quantity
	o, err := order.NewOrder(order.Order{
		Request:          req,
		BrokerOrderID:    "async-" + req.OrderID.String(),
		AcceptedQuantity: &accepted,
		Status:           order.StatusWorking,
	})
	if err != nil {
		return order.Order{}, err
	}
	return o, nil
}

func (a notSynchronouslyFillingAccount) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	return a.inner.Cancel(ctx, req)
}

func (a notSynchronouslyFillingAccount) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	return a.inner.Replace(ctx, req)
}

func (a notSynchronouslyFillingAccount) Events(ctx context.Context, cursor brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	return a.inner.Events(ctx, cursor)
}

var _ brokerpkg.Broker = notSynchronouslyFillingBroker{}
var _ brokerpkg.Account = notSynchronouslyFillingAccount{}

// TestPipelineSubmit_BracketEntryNotSynchronouslyFilledReturnsError
// is this issue's own central safety proof: against a broker whose
// Submit does not report an immediate fill — every real broker
// adapter today — a bracket intent fails loudly with
// ErrBracketEntryNotSynchronouslyFilled rather than silently
// proceeding as if protected. This is exactly ADR-059's own
// deliberate backtest-only scope boundary, verified directly rather
// than only asserted in documentation.
func TestPipelineSubmit_BracketEntryNotSynchronouslyFilledReturnsError(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clock, IDs: h.ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine()
	require.NoError(t, err)
	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  notSynchronouslyFillingBroker{inner: h.broker},
		IDs:     h.ids,
	})
	require.NoError(t, err)

	snap := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterWithStopIntent(t, h.ids, h.listing.InstrumentID(), order.Buy, "1.05000")

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         snap,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, pipeline.ErrBracketEntryNotSynchronouslyFilled)
	require.NotNil(t, result.Bracket)
	assert.Equal(t, order.StatusWorking, result.Bracket.Entry.Order.Status)
	assert.Equal(t, pipeline.Result{}, result.Bracket.Stop, "the stop leg must never be attempted when the entry did not fill synchronously")
}
