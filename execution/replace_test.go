package execution

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlanReplace_RatchetsExistingStop is issue #336's central case:
// with a resting Stop order already in place, PlanReplace returns a
// validated ReplaceRequest naming that order and moving it to the
// intent's new StopPrice.
func TestPlanReplace_RatchetsExistingStop(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	existing := mustRestingStopOrder(t, deps.IDs, accountID, listing, order.Sell, "1000", "1.05000")
	snap := mustSnapshotWithOpenOrders(t, accountID, listing, []order.Position{pos}, []order.Order{existing})
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID()) // StopPrice 1.05000, see mustAdjustStopIntent

	result, err := p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)
	assert.Equal(t, existing.Request.OrderID, result.Request.OrderID)
	require.NotNil(t, result.Request.NewStopPrice)
	assert.Equal(t, intent.StopPrice.String(), result.Request.NewStopPrice.String())
	assert.Nil(t, result.Request.NewQuantity, "PlanReplace never changes quantity, only StopPrice")
	assert.Nil(t, result.Request.NewLimitPrice)
	assert.Equal(t, intent.Metadata.CorrelationID, result.Request.Metadata.CorrelationID)
	assert.Equal(t, intent.Metadata.EventID, result.Request.Metadata.CausationID)
}

// TestPlanReplace_NoRestingStopOrder is PlanReplace's own boundary
// case, the mirror image of Plan(IntentAdjustStop)'s
// ErrExistingStopOrder: nothing to replace means Plan should have been
// called instead.
func TestPlanReplace_NoRestingStopOrder(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	_, err = p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrNoRestingStopOrder)
}

// TestPlanReplace_RejectsNonAdjustStopIntent proves PlanReplace
// supports only order.IntentAdjustStop, per its own doc comment and
// ADR-054's scope decision.
func TestPlanReplace_RejectsNonAdjustStopIntent(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	snap := mustSnapshot(t, accountID, listing, pos)
	intent := mustExitIntent(t, deps.IDs, listing.InstrumentID())

	_, err = p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrUnsupportedIntentKind)
}

// TestPlanReplace_RejectsListingInstrumentMismatch mirrors
// TestPlanRejectsListingInstrumentMismatch for PlanReplace's own input
// validation.
func TestPlanReplace_RejectsListingInstrumentMismatch(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	snap := mustSnapshot(t, accountID, listing)

	other := mustGbpUsdInstrumentID(t)
	intent := mustAdjustStopIntent(t, deps.IDs, other)

	_, err = p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrInvalidPlanInput)
}

// TestPlanReplace_RejectsUnconstructedListing proves ReplaceInput
// validation runs even before the resting-order lookup.
func TestPlanReplace_RejectsUnconstructedListing(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	snap := mustSnapshot(t, accountID, listing)
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	_, err = p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Account: snap})
	require.ErrorIs(t, err, ErrInvalidPlanInput)
}

// TestPlanReplace_RejectsUnconstructedAccount mirrors
// TestPlanRejectsUnconstructedAccount for PlanReplace (PR #337 review).
func TestPlanReplace_RejectsUnconstructedAccount(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	_, err = p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing})
	require.ErrorIs(t, err, ErrInvalidPlanInput)
}

// TestPlanReplace_RejectsListingProviderAccountBrokerMismatch mirrors
// TestPlanRejectsListingProviderAccountBrokerMismatch for PlanReplace
// (PR #337 review): a Listing whose provider does not match the
// account's own broker must be rejected before ever reaching the
// resting-order lookup, the same as Plan already requires.
func TestPlanReplace_RejectsListingProviderAccountBrokerMismatch(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	simListing := mustEurUsdListingForProvider(t, "sim")
	otherListing := mustEurUsdListingForProvider(t, "alpaca")
	accountID := mustAccountID(t, deps.IDs)
	snap := mustSnapshot(t, accountID, simListing) // Broker() == "sim"
	intent := mustAdjustStopIntent(t, deps.IDs, otherListing.InstrumentID())

	_, err = p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: otherListing, Account: snap})
	require.ErrorIs(t, err, ErrInvalidPlanInput)
}

// TestPlanReplace_HonorsContextCancellation matches Plan's own
// cancellation contract.
func TestPlanReplace_HonorsContextCancellation(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	existing := mustRestingStopOrder(t, deps.IDs, accountID, listing, order.Sell, "1000", "1.05000")
	snap := mustSnapshotWithOpenOrders(t, accountID, listing, []order.Position{pos}, []order.Order{existing})
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.PlanReplace(ctx, ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, context.Canceled)
}
