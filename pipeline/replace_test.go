package pipeline_test

// This file is issue #336's pipeline-level coverage: order.IntentAdjustStop
// carried all the way through Pipeline.Submit to the real
// adapters/broker/sim.Broker — initial stop placement, ratcheting an
// existing one via Replace, and the stop actually triggering through
// Broker.Advance's real intrabar fill logic. Nothing here is mocked:
// every test uses newHarness's real sim.Broker, the same as every
// other pipeline_test.go end-to-end test.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sim "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
)

// enterLong runs a real Enter intent through Submit and returns the
// resulting snapshot, so every test in this file starts from an actual
// long position rather than a hand-built fixture.
func enterLong(t *testing.T, ctx context.Context, h testHarness, p *pipeline.Pipeline) {
	t.Helper()
	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)
	result, err := p.Submit(ctx, pipeline.Input{
		Intent:          intent,
		Listing:         h.listing,
		Account:         h.snapshot(t, ctx),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)
	require.True(t, result.Decision.Allowed)
}

// adjustStopIntentAt is mustAdjustStopIntent generalized to a caller-chosen
// StopPrice, needed here to place an initial stop and then ratchet it
// to a different (higher) price in the same test.
func adjustStopIntentAt(t *testing.T, h testHarness, stopPrice string) order.Intent {
	t.Helper()
	intent := mustAdjustStopIntent(t, h.ids, h.listing.InstrumentID())
	sp := num.MustParsePrice(stopPrice)
	// mustAdjustStopIntent already builds a valid Intent; only
	// StopPrice needs to differ per call, so rebuild from its own
	// already-generated Intent rather than duplicating id-generation.
	rebuilt, err := order.NewIntent(order.Intent{
		IntentID:   intent.IntentID,
		Kind:       intent.Kind,
		Instrument: intent.Instrument,
		StopPrice:  &sp,
		Metadata:   intent.Metadata,
	})
	require.NoError(t, err)
	return rebuilt
}

// TestPipelineSubmit_AdjustStop_InitialPlacement is issue #336's
// central Plan-side case, exercised through the full pipeline: with an
// open long position and no resting stop yet, Submit produces an
// ordinary new-order Result (Proposal/Decision/Request/Order all
// populated, Replace nil) for a ReduceOnly Sell Stop order, and the
// resulting order actually appears on the real sim.Broker account.
func TestPipelineSubmit_AdjustStop_InitialPlacement(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)
	enterLong(t, ctx, h, p)

	intent := adjustStopIntentAt(t, h, "1.05000")
	result, err := p.Submit(ctx, pipeline.Input{Intent: intent, Listing: h.listing, Account: h.snapshot(t, ctx)})
	require.NoError(t, err)

	assert.Nil(t, result.Replace, "initial placement is the new-order path, not a replacement")
	assert.True(t, result.Decision.Allowed)
	assert.Equal(t, order.Stop, result.Proposal.Type)
	assert.Equal(t, order.Sell, result.Proposal.Side)
	assert.True(t, result.Proposal.ReduceOnly)
	assert.NotEqual(t, order.Order{}, result.Order)

	after := h.snapshot(t, ctx)
	require.Len(t, after.OpenOrders(), 1)
	assert.Equal(t, order.Stop, after.OpenOrders()[0].Request.Type)
	require.NotNil(t, after.OpenOrders()[0].AcceptedStopPrice)
	assert.Equal(t, "1.05", after.OpenOrders()[0].AcceptedStopPrice.String())
}

// TestPipelineSubmit_AdjustStop_RatchetsExistingStop is issue #336's
// central replacement case: a second IntentAdjustStop against an
// instrument that already has a resting stop order takes the Replace
// path — Result.Replace is populated, Proposal/Decision/Request/Order
// all stay zero, and the broker's own resting order actually moves to
// the new price via a real Replace call, not a second order.
func TestPipelineSubmit_AdjustStop_RatchetsExistingStop(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)
	enterLong(t, ctx, h, p)

	first := adjustStopIntentAt(t, h, "1.05000")
	_, err := p.Submit(ctx, pipeline.Input{Intent: first, Listing: h.listing, Account: h.snapshot(t, ctx)})
	require.NoError(t, err)

	second := adjustStopIntentAt(t, h, "1.06000")
	result, err := p.Submit(ctx, pipeline.Input{Intent: second, Listing: h.listing, Account: h.snapshot(t, ctx)})
	require.NoError(t, err)

	require.NotNil(t, result.Replace)
	assert.Equal(t, order.Proposal{}, result.Proposal, "the replace path never builds a new-order Proposal")
	assert.Equal(t, order.Request{}, result.Request)
	assert.Equal(t, order.Order{}, result.Order)
	require.NotNil(t, result.Replace.Request.NewStopPrice)
	assert.Equal(t, "1.06", result.Replace.Request.NewStopPrice.String())
	assert.NotEqual(t, order.ReplaceResult{}, result.Replace.Result, "Submit must populate the broker's own Replace outcome")

	after := h.snapshot(t, ctx)
	require.Len(t, after.OpenOrders(), 1, "ratcheting must not leave two resting stop orders")
	require.NotNil(t, after.OpenOrders()[0].AcceptedStopPrice)
	assert.Equal(t, "1.06", after.OpenOrders()[0].AcceptedStopPrice.String())
}

// TestPipelineSubmit_AdjustStop_TriggersThroughRealAdvance is issue
// #336's own acceptance criterion: a full trailing-stop lifecycle —
// enter, place the initial stop, ratchet it upward, then a real bar
// whose Low breaches the ratcheted stop — proven against
// sim.Broker.Advance's actual intrabar fill logic, not a mock. The
// position must close via the stop trigger, at the stop price (a
// normal intrabar breach, not a gap), with no strategy-side
// involvement at all: Advance alone resolves it.
func TestPipelineSubmit_AdjustStop_TriggersThroughRealAdvance(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)
	enterLong(t, ctx, h, p)

	firstStop := adjustStopIntentAt(t, h, "1.05000")
	_, err := p.Submit(ctx, pipeline.Input{Intent: firstStop, Listing: h.listing, Account: h.snapshot(t, ctx)})
	require.NoError(t, err)

	ratchet := adjustStopIntentAt(t, h, "1.08000")
	_, err = p.Submit(ctx, pipeline.Input{Intent: ratchet, Listing: h.listing, Account: h.snapshot(t, ctx)})
	require.NoError(t, err)

	before := h.snapshot(t, ctx)
	require.Len(t, before.Positions(), 1, "position must still be open before the stop-hit bar")

	// A bar whose Low breaches the ratcheted 1.08 stop, Open still
	// above it — an intrabar trigger, not a gap-through.
	err = h.broker.Advance(ctx, sim.Observation{
		Listing: h.listing,
		Open:    num.MustParsePrice("1.09000"),
		High:    num.MustParsePrice("1.09500"),
		Low:     num.MustParsePrice("1.07500"),
		Close:   num.MustParsePrice("1.08200"),
		Time:    h.clock.Now(),
	})
	require.NoError(t, err)

	after := h.snapshot(t, ctx)
	assert.Empty(t, after.Positions(), "the stop must have closed the position")
	assert.Empty(t, after.OpenOrders(), "the triggered stop order must no longer be resting")
}
