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
	assert.Nil(t, result.Request.NewQuantity, "position size unchanged from the resting order's own accepted quantity: no NewQuantity needed")
	assert.Nil(t, result.Request.NewLimitPrice)
	assert.Equal(t, intent.Metadata.CorrelationID, result.Request.Metadata.CorrelationID)
	assert.Equal(t, intent.Metadata.EventID, result.Request.Metadata.CausationID)
}

// TestPlanReplace_SyncsQuantityWhenPositionGrew is PR #337 review's
// own concern, addressed directly: issue #336's own non-goals scoped
// in exactly this — "quantity-changing replaces beyond what's needed
// to keep a protective stop's quantity in sync with the current
// position size" are out of scope, meaning staying in sync is not. A
// resting stop placed for 1000 units, with the position now at 1500
// (for example after scaling in), must have its own quantity
// resynchronized alongside any stop-price ratchet.
func TestPlanReplace_SyncsQuantityWhenPositionGrew(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1500")
	existing := mustRestingStopOrder(t, deps.IDs, accountID, listing, order.Sell, "1000", "1.05000")
	snap := mustSnapshotWithOpenOrders(t, accountID, listing, []order.Position{pos}, []order.Order{existing})
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	result, err := p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)
	require.NotNil(t, result.Request.NewQuantity)
	assert.Equal(t, "1500", result.Request.NewQuantity.String())
	require.NotNil(t, result.Request.NewStopPrice, "a quantity resync does not drop the stop-price ratchet")
	assert.Equal(t, intent.StopPrice.String(), result.Request.NewStopPrice.String())
}

// TestPlanReplace_NoQuantityChangeWhenPositionUnchanged is the
// negative case alongside TestPlanReplace_SyncsQuantityWhenPositionGrew:
// TestPlanReplace_RatchetsExistingStop already covers this implicitly
// (position and resting order both at 1000), named explicitly here so
// the "no spurious NewQuantity" behavior has its own test, not just an
// incidental assertion in a differently-named test.
func TestPlanReplace_NoQuantityChangeWhenPositionUnchanged(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	existing := mustRestingStopOrder(t, deps.IDs, accountID, listing, order.Sell, "1000", "1.05000")
	snap := mustSnapshotWithOpenOrders(t, accountID, listing, []order.Position{pos}, []order.Order{existing})
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	result, err := p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)
	assert.Nil(t, result.Request.NewQuantity)
}

// TestPlanReplace_IgnoresNonReduceOnlyStopOrder and
// TestPlanReplace_IgnoresWrongSideStopOrder are PR #337 review's own
// "findRestingStopOrder should mean protective stop, not any Stop
// order" request: a resting Stop order that isn't ReduceOnly, or whose
// Side would open exposure rather than protect the current position,
// must not be found as this instrument's protective stop —
// PlanReplace must report ErrNoRestingStopOrder exactly as if no Stop
// order existed at all.
func TestPlanReplace_IgnoresNonReduceOnlyStopOrder(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	notReduceOnly := mustNonProtectiveStopOrder(t, deps.IDs, accountID, listing, order.Sell, "1000", "1.05000", false)
	snap := mustSnapshotWithOpenOrders(t, accountID, listing, []order.Position{pos}, []order.Order{notReduceOnly})
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	_, err = p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrNoRestingStopOrder)
}

func TestPlanReplace_IgnoresWrongSideStopOrder(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	// A Buy Stop while long does not protect this position (a Sell
	// Stop would); it is left resting as if from a stale/unrelated order.
	wrongSide := mustNonProtectiveStopOrder(t, deps.IDs, accountID, listing, order.Buy, "1000", "1.05000", true)
	snap := mustSnapshotWithOpenOrders(t, accountID, listing, []order.Position{pos}, []order.Order{wrongSide})
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	_, err = p.PlanReplace(context.Background(), ReplaceInput{Intent: intent, Listing: listing, Account: snap})
	require.ErrorIs(t, err, ErrNoRestingStopOrder)
}

// TestPlanAdjustStop_IgnoresNonProtectiveStopOrder proves the same
// hardening on Plan's own IntentAdjustStop dispatch: a non-protective
// resting Stop order must not trip ErrExistingStopOrder — Plan must
// still place a fresh, real protective stop.
func TestPlanAdjustStop_IgnoresNonProtectiveStopOrder(t *testing.T) {
	deps := testDeps()
	p, err := NewPlanner(deps)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t, deps.IDs)
	pos := mustPosition(t, accountID, listing, order.Long, "1000")
	notReduceOnly := mustNonProtectiveStopOrder(t, deps.IDs, accountID, listing, order.Sell, "1000", "1.05000", false)
	snap := mustSnapshotWithOpenOrders(t, accountID, listing, []order.Position{pos}, []order.Order{notReduceOnly})
	intent := mustAdjustStopIntent(t, deps.IDs, listing.InstrumentID())

	result, err := p.Plan(context.Background(), PlanInput{Intent: intent, Listing: listing, Account: snap})
	require.NoError(t, err)
	assert.Equal(t, order.Stop, result.Proposal.Type)
	assert.True(t, result.Proposal.ReduceOnly)
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
