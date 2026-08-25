package execution

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlannerRejectsMissingDeps(t *testing.T) {
	_, err := NewPlanner(Deps{})
	require.ErrorIs(t, err, ErrInvalidDeps)

	_, err = NewPlanner(Deps{Clock: testDeps().Clock})
	require.ErrorIs(t, err, ErrInvalidDeps)

	_, err = NewPlanner(Deps{IDs: testDeps().IDs})
	require.ErrorIs(t, err, ErrInvalidDeps)
}

func TestPlanEnter(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	intent := mustEnterIntent(t, deps.IDs, listing.InstrumentID(), order.Buy)
	snap := mustSnapshot(t, accountID, listing)

	result, err := p.Plan(context.Background(), PlanInput{
		Intent: intent, Listing: listing, Account: snap, Quantity: qty(t, "1000"),
	})
	require.NoError(t, err)

	prop := result.Proposal
	assert.Equal(t, order.Buy, prop.Side)
	assert.Equal(t, order.Market, prop.Type)
	assert.Equal(t, order.GTC, prop.TimeInForce)
	assert.False(t, prop.ReduceOnly)
	assert.True(t, prop.Quantity.Equal(*qty(t, "1000")))
	assert.Equal(t, accountID, prop.AccountID)
	assert.Equal(t, intent.Metadata.CorrelationID, prop.Metadata.CorrelationID)
	assert.Equal(t, intent.Metadata.EventID, prop.Metadata.CausationID)
	assert.False(t, prop.Metadata.EventID.IsZero())
}

func TestPlanEnterRequiresQuantity(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	intent := mustEnterIntent(t, deps.IDs, listing.InstrumentID(), order.Buy)
	snap := mustSnapshot(t, accountID, listing)

	_, err = p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrInvalidPlanInput)
}

func TestPlanExitClosesLongPosition(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustExitIntent(t, deps.IDs, listing.InstrumentID())

	result, err := p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)

	assert.Equal(t, order.Sell, result.Proposal.Side)
	assert.True(t, result.Proposal.ReduceOnly)
	assert.True(t, result.Proposal.Quantity.Equal(*qty(t, "1000")))
}

func TestPlanExitClosesShortPosition(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Short, "500")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustExitIntent(t, deps.IDs, listing.InstrumentID())

	result, err := p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)

	assert.Equal(t, order.Buy, result.Proposal.Side)
	assert.True(t, result.Proposal.Quantity.Equal(*qty(t, "500")))
}

func TestPlanExitRejectsQuantityInput(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	intent := mustExitIntent(t, deps.IDs, listing.InstrumentID())
	snap := mustSnapshot(t, accountID, listing)

	_, err = p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap, Quantity: qty(t, "1")})
	require.ErrorIs(t, err, ErrInvalidPlanInput)
}

func TestPlanExitNoPositionReturnsClassifiableError(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	snap := mustSnapshot(t, accountID, listing)
	intent := mustExitIntent(t, deps.IDs, listing.InstrumentID())

	_, err = p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrNoPositionToExit)
}

func TestPlanTargetExposureFromFlat(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	snap := mustSnapshot(t, accountID, listing)
	intent := mustTargetExposureIntent(t, deps.IDs, listing.InstrumentID(), order.Buy, "1000")

	result, err := p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)

	assert.Equal(t, order.Buy, result.Proposal.Side)
	assert.False(t, result.Proposal.ReduceOnly)
	assert.True(t, result.Proposal.Quantity.Equal(*qty(t, "1000")))
}

func TestPlanTargetExposureGrowsSameDirection(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustTargetExposureIntent(t, deps.IDs, listing.InstrumentID(), order.Buy, "800")

	result, err := p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)

	assert.Equal(t, order.Buy, result.Proposal.Side)
	assert.False(t, result.Proposal.ReduceOnly)
	assert.True(t, result.Proposal.Quantity.Equal(*qty(t, "300")), "got %s", result.Proposal.Quantity)
}

func TestPlanTargetExposureShrinksSameDirection(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustTargetExposureIntent(t, deps.IDs, listing.InstrumentID(), order.Buy, "300")

	result, err := p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)

	assert.Equal(t, order.Sell, result.Proposal.Side, "shrinking a long position sells, but stays reduce-only")
	assert.True(t, result.Proposal.ReduceOnly)
	assert.True(t, result.Proposal.Quantity.Equal(*qty(t, "200")), "got %s", result.Proposal.Quantity)
}

func TestPlanTargetExposureShrinksShortPosition(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Short, "500")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustTargetExposureIntent(t, deps.IDs, listing.InstrumentID(), order.Sell, "300")

	result, err := p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)

	assert.Equal(t, order.Buy, result.Proposal.Side, "shrinking a short position buys, but stays reduce-only")
	assert.True(t, result.Proposal.ReduceOnly)
	assert.True(t, result.Proposal.Quantity.Equal(*qty(t, "200")), "got %s", result.Proposal.Quantity)
}

func TestPlanTargetExposureReversesDirection(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustTargetExposureIntent(t, deps.IDs, listing.InstrumentID(), order.Sell, "200")

	result, err := p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)

	assert.Equal(t, order.Sell, result.Proposal.Side)
	assert.False(t, result.Proposal.ReduceOnly, "reversing through flat opens new exposure, not merely reducing")
	assert.True(t, result.Proposal.Quantity.Equal(*qty(t, "700")), "500 to close + 200 to open short = 700; got %s", result.Proposal.Quantity)
}

func TestPlanTargetExposureAlreadyAtTarget(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustTargetExposureIntent(t, deps.IDs, listing.InstrumentID(), order.Buy, "500")

	_, err = p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrAlreadyAtTarget)
}

func TestPlanAdjustStopIsUnsupported(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	snap := mustSnapshot(t, accountID, listing)
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	_, err = p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrUnsupportedIntentKind)
}

func TestPlanRejectsListingInstrumentMismatch(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	snap := mustSnapshot(t, accountID, listing)

	other := mustGbpUsdInstrumentID(t)
	intent := mustEnterIntent(t, deps.IDs, other, order.Buy)

	_, err = p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap, Quantity: qty(t, "100")})
	require.ErrorIs(t, err, ErrInvalidPlanInput)
}

func TestPlanRejectsUnconstructedAccount(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	intent := mustEnterIntent(t, deps.IDs, listing.InstrumentID(), order.Buy)

	_, err = p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Quantity: qty(t, "100")})
	require.ErrorIs(t, err, ErrInvalidPlanInput)
}

func TestPlanPropagatesCancelledContext(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	snap := mustSnapshot(t, accountID, listing)
	intent := mustEnterIntent(t, deps.IDs, listing.InstrumentID(), order.Buy)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = p.Plan(ctx, PlanInput{Intent: intent, Listing: listing, Account: snap, Quantity: qty(t, "100")})
	require.ErrorIs(t, err, context.Canceled)
}

// TestPlanIsDeterministicAcrossIndependentPlanners proves the
// determinism Planner's own doc comment promises: two independently
// constructed Planners, each with fresh but identically-seeded Deps,
// produce byte-identical PlanResults for the same PlanInput.
func TestPlanIsDeterministicAcrossIndependentPlanners(t *testing.T) {
	buildAndPlan := func(t *testing.T) PlanResult {
		t.Helper()
		deps := testDeps()
		p, err := NewPlanner(deps)
		require.NoError(t, err)

		listing := mustEurUsdListing(t)
		accountID := mustAccountID(t, deps.IDs)
		snap := mustSnapshot(t, accountID, listing)
		intent := mustEnterIntent(t, deps.IDs, listing.InstrumentID(), order.Buy)

		result, err := p.Plan(context.Background(), PlanInput{
			Intent: intent, Listing: listing, Account: snap, Quantity: qty(t, "1000"),
		})
		require.NoError(t, err)
		return result
	}

	first := buildAndPlan(t)
	second := buildAndPlan(t)
	require.Equal(t, first, second)
}
