package pipeline_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	sim "github.com/rustyeddy/trader/adapters/broker/sim"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
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

var testStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func mustEurUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// fixedPriceSource is a deterministic sim.FillPriceSource keyed by
// listing symbol.
type fixedPriceSource map[string]num.Price

func (f fixedPriceSource) Info() sim.ModelInfo {
	return sim.ModelInfo{Name: "fixedPriceSource", Version: "test"}
}

func (f fixedPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := f[listing.Symbol()]
	if !ok {
		return num.Price{}, errors.New("fixedPriceSource: no price")
	}
	return p, nil
}

// testHarness bundles one deterministic sim.Broker/Pipeline pairing,
// so every test constructs its own isolated instance rather than
// sharing mutable broker state across test cases.
type testHarness struct {
	broker    *sim.Broker
	accountID id.AccountID
	listing   instrument.Listing
	ids       *id.Generator
	clock     clock.Clock
}

func newHarness(t *testing.T, startingCash string) testHarness {
	t.Helper()
	c := clock.NewSimulated(testStart)
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(ids)
	require.NoError(t, err)

	b, err := sim.NewBroker("sim", sim.Deps{
		Clock: c,
		IDs:   ids,
		Prices: fixedPriceSource{
			"EUR_USD": num.MustParsePrice("1.10000"),
		},
	}, sim.AccountConfig{
		AccountID:    accountID,
		StartingCash: num.MustParseMoney(startingCash, num.MustParseCurrency("USD")),
	})
	require.NoError(t, err)

	return testHarness{
		broker:    b,
		accountID: accountID,
		listing:   mustEurUsdListing(t),
		ids:       ids,
		clock:     c,
	}
}

func (h testHarness) snapshot(t *testing.T, ctx context.Context) account.Snapshot {
	t.Helper()
	acc, err := h.broker.OpenAccount(ctx, h.accountID)
	require.NoError(t, err)
	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	return snap
}

func mustEnterIntent(t *testing.T, ids *id.Generator, instID instrument.ID, side order.Side) order.Intent {
	t.Helper()
	intentID, err := id.GenerateIntentID(ids)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(ids)
	require.NoError(t, err)
	corrID, err := id.GenerateCorrelationID(ids)
	require.NoError(t, err)
	in, err := order.NewIntent(order.Intent{
		IntentID:   intentID,
		Kind:       order.IntentEnter,
		Instrument: instID,
		Side:       side,
		Metadata:   id.Metadata{EventID: eventID, CorrelationID: corrID},
	})
	require.NoError(t, err)
	return in
}

func mustAdjustStopIntent(t *testing.T, ids *id.Generator, instID instrument.ID) order.Intent {
	t.Helper()
	intentID, err := id.GenerateIntentID(ids)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(ids)
	require.NoError(t, err)
	corrID, err := id.GenerateCorrelationID(ids)
	require.NoError(t, err)
	sp := num.MustParsePrice("1.05000")
	in, err := order.NewIntent(order.Intent{
		IntentID:   intentID,
		Kind:       order.IntentAdjustStop,
		Instrument: instID,
		StopPrice:  &sp,
		Metadata:   id.Metadata{EventID: eventID, CorrelationID: corrID},
	})
	require.NoError(t, err)
	return in
}

// newPipeline builds a Pipeline over h's broker with a fresh
// execution.Planner/risk.Engine/risk.Sizer, using ids/clock so a
// caller can construct two independent harnesses sharing the same
// deterministic seed for a cross-instance determinism check.
func newPipeline(t *testing.T, h testHarness, rules ...risk.Rule) *pipeline.Pipeline {
	t.Helper()
	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clock, IDs: h.ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine(rules...)
	require.NoError(t, err)
	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  h.broker,
		IDs:     h.ids,
	})
	require.NoError(t, err)
	return p
}

func TestPipelineSubmit_EndToEndAgainstSimulator(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)

	snap := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         snap,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)

	assert.True(t, result.Decision.Allowed)
	assert.Equal(t, order.Buy, result.Proposal.Side)
	assert.False(t, result.Proposal.Quantity.IsZero())
	assert.NotEqual(t, order.Order{}, result.Order)
	assert.Equal(t, result.Proposal.Quantity, result.Order.Request.Quantity)
	assert.Equal(t, intent.Metadata.CorrelationID, result.Proposal.Metadata.CorrelationID)

	after := h.snapshot(t, ctx)
	require.Len(t, after.Positions(), 1)
	assert.Equal(t, order.Long, after.Positions()[0].Side)
}

func TestPipelineSubmit_RiskRejectionNeverReachesBroker(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	rejecting := rejectingRule{}
	p := newPipeline(t, h, rejecting)

	before := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         before,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, pipeline.ErrRejected)
	assert.False(t, result.Decision.Allowed)
	assert.NotEqual(t, order.Proposal{}, result.Proposal, "Proposal is still populated on rejection")
	assert.Equal(t, order.Order{}, result.Order, "Order must never be populated on rejection")

	after := h.snapshot(t, ctx)
	assert.Empty(t, after.Positions(), "broker must never be mutated by a rejected proposal")
	assert.Empty(t, after.OpenOrders())
}

// rejectingRule is a minimal risk.Rule that always reports a
// violation, used to exercise Submit's rejection path without
// depending on any concrete production Rule's own numeric thresholds.
type rejectingRule struct{}

func (rejectingRule) Name() string { return "always_reject" }
func (rejectingRule) Evaluate(ctx context.Context, in risk.Input) (risk.RuleResult, error) {
	return risk.RuleResult{Violations: []risk.Violation{{Message: "test: always rejects"}}}, nil
}

func TestPipelineSubmit_UnsupportedIntentKindPropagatesPlanningError(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)

	before := h.snapshot(t, ctx)
	intent := mustAdjustStopIntent(t, h.ids, h.listing.InstrumentID())

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:  intent,
		Listing: h.listing,
		Account: before,
	})
	require.ErrorIs(t, err, execution.ErrUnsupportedIntentKind)
	assert.Equal(t, pipeline.Result{}, result)

	after := h.snapshot(t, ctx)
	assert.Empty(t, after.Positions(), "broker must never be touched on a planning failure")
}

func TestPipelineSubmit_EnterWithoutAdverseDistanceIsInvalidInput(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)

	before := h.snapshot(t, ctx)
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	_, err := p.Submit(ctx, pipeline.Input{
		Intent:       intent,
		Listing:      h.listing,
		Account:      before,
		RiskFraction: num.MustParseRate("0.01"),
	})
	require.ErrorIs(t, err, pipeline.ErrInvalidInput)
}

func TestPipelineSubmit_SizingErrorPropagates(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)

	before := h.snapshot(t, ctx)
	// An enormous stop distance drives the sized quantity to zero at
	// the listing's own quantity increment.
	adverse := num.MustParsePrice("1000000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	_, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         before,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, risk.ErrSizeRoundsToZero)

	after := h.snapshot(t, ctx)
	assert.Empty(t, after.Positions())
}

func TestPipelineSubmit_InvalidIntentIsInvalidInput(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)

	before := h.snapshot(t, ctx)
	_, err := p.Submit(ctx, pipeline.Input{
		Intent:  order.Intent{}, // zero value: fails order.NewIntent
		Listing: h.listing,
		Account: before,
	})
	require.ErrorIs(t, err, pipeline.ErrInvalidInput)
}

func TestPipelineSubmit_PropagatesCancelledContext(t *testing.T) {
	h := newHarness(t, "10000")
	p := newPipeline(t, h)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	before := h.snapshot(t, context.Background())
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)
	adverse := num.MustParsePrice("0.01000")

	_, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         before,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestPipelineSubmit_DeterministicAcrossIndependentInstances(t *testing.T) {
	ctx := context.Background()
	adverse := num.MustParsePrice("0.01000")

	run := func() pipeline.Result {
		h := newHarness(t, "10000")
		p := newPipeline(t, h)
		snap := h.snapshot(t, ctx)
		intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)
		result, err := p.Submit(ctx, pipeline.Input{
			Intent:          intent,
			Listing:         h.listing,
			Account:         snap,
			RiskFraction:    num.MustParseRate("0.01"),
			AdverseDistance: &adverse,
		})
		require.NoError(t, err)
		return result
	}

	a := run()
	b := run()
	assert.Equal(t, a.Proposal, b.Proposal)
	assert.Equal(t, a.Decision, b.Decision)
	assert.Equal(t, a.Order, b.Order)
}

func TestNewPipelineRejectsIncompleteDeps(t *testing.T) {
	h := newHarness(t, "10000")
	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clock, IDs: h.ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine()
	require.NoError(t, err)
	sizer := risk.NewFixedFractionSizer()

	tests := map[string]pipeline.Deps{
		"no sizer":   {Planner: planner, Engine: engine, Broker: h.broker, IDs: h.ids},
		"no planner": {Sizer: sizer, Engine: engine, Broker: h.broker, IDs: h.ids},
		"no engine":  {Sizer: sizer, Planner: planner, Broker: h.broker, IDs: h.ids},
		"no broker":  {Sizer: sizer, Planner: planner, Engine: engine, IDs: h.ids},
		"no ids":     {Sizer: sizer, Planner: planner, Engine: engine, Broker: h.broker},
	}
	for name, deps := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := pipeline.NewPipeline(deps)
			require.ErrorIs(t, err, pipeline.ErrInvalidDeps)
		})
	}
}

var _ brokerpkg.Broker = (*sim.Broker)(nil)

func TestPipelineSubmit_OpenAccountErrorPropagates(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clock, IDs: h.ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine()
	require.NoError(t, err)
	openErr := errors.New("boom: open account")
	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  failingBroker{openErr: openErr},
		IDs:     h.ids,
	})
	require.NoError(t, err)

	before := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         before,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, openErr)
	assert.True(t, result.Decision.Allowed, "decision is still populated on a broker-side failure")
	assert.Equal(t, order.Order{}, result.Order)
}

func TestPipelineSubmit_SubmitErrorPropagates(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clock, IDs: h.ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine()
	require.NoError(t, err)
	submitErr := errors.New("boom: submit")
	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  failingBroker{submitErr: submitErr},
		IDs:     h.ids,
	})
	require.NoError(t, err)

	before := h.snapshot(t, ctx)
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         before,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, submitErr)
	assert.True(t, result.Decision.Allowed)
	assert.Equal(t, order.Order{}, result.Order)
}
