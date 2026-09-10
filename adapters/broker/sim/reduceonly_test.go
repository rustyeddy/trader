package sim

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustReduceOnlyMarketRequestFor is mustMarketRequestFor with
// ReduceOnly set — issue #352's own enforcement is scoped to exactly
// this field, so every test in this file constructs requests through
// here (or mustReduceOnlyStopRequestFor below) rather than
// mustMarketRequest/mustStopRequest, which never set it.
func mustReduceOnlyMarketRequestFor(t *testing.T, gen *id.Generator, accountID id.AccountID, listing instrument.Listing, side order.Side, quantity string) order.Request {
	t.Helper()
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        side,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity(quantity),
		ReduceOnly:  true,
		Metadata:    id.Metadata{EventID: mustEventID(t, gen)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, gen))
	require.NoError(t, err)
	return req
}

func mustReduceOnlyStopRequestFor(t *testing.T, gen *id.Generator, accountID id.AccountID, listing instrument.Listing, side order.Side, quantity, stopPrice string) order.Request {
	t.Helper()
	price := num.MustParsePrice(stopPrice)
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        side,
		Type:        order.Stop,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity(quantity),
		StopPrice:   &price,
		ReduceOnly:  true,
		Metadata:    id.Metadata{EventID: mustEventID(t, gen)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, gen))
	require.NoError(t, err)
	return req
}

// openLongPosition submits a plain (non-ReduceOnly) market Buy for
// quantity, establishing a Long position the rest of a test then
// exercises a ReduceOnly order against.
func openLongPosition(t *testing.T, ctx context.Context, acc interface {
	Submit(context.Context, order.Request) (order.Order, error)
}, gen *id.Generator, accountID id.AccountID, quantity string) {
	t.Helper()
	_, err := acc.Submit(ctx, mustMarketRequest(t, gen, accountID, order.Buy, quantity))
	require.NoError(t, err)
}

func openShortPosition(t *testing.T, ctx context.Context, acc interface {
	Submit(context.Context, order.Request) (order.Order, error)
}, gen *id.Generator, accountID id.AccountID, quantity string) {
	t.Helper()
	_, err := acc.Submit(ctx, mustMarketRequest(t, gen, accountID, order.Sell, quantity))
	require.NoError(t, err)
}

// TestReduceOnly_SellAgainstLongEqualQuantityClosesToFlat is the
// baseline sanity case (issue #352's own acceptance list): a
// ReduceOnly Sell for exactly the held Long quantity closes to Flat,
// unaffected by the new enforcement.
func TestReduceOnly_SellAgainstLongEqualQuantityClosesToFlat(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	openLongPosition(t, ctx, acc, deps.IDs, accountID, "1000")

	o, err := acc.Submit(ctx, mustReduceOnlyMarketRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Sell, "1000"))
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, o.Status)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions())
}

// TestReduceOnly_SellAgainstFlatNeverOpensShort is issue #352's own
// core invariant: a ReduceOnly Sell with no position at all must
// never fill and open a Short — it is canceled instead.
func TestReduceOnly_SellAgainstFlatNeverOpensShort(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	o, err := acc.Submit(ctx, mustReduceOnlyMarketRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Sell, "1000"))
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, o.Status, "a reduce-only order with nothing to reduce must be canceled, not filled")

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions(), "must never open a Short from a reduce-only order")
}

// TestReduceOnly_SellAgainstShortNeverIncreasesShort proves the
// direction check, not just "no position at all": a ReduceOnly Sell
// is a Short-increasing side, so it can never reduce an existing
// Short either.
func TestReduceOnly_SellAgainstShortNeverIncreasesShort(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	openShortPosition(t, ctx, acc, deps.IDs, accountID, "1000")

	o, err := acc.Submit(ctx, mustReduceOnlyMarketRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Sell, "500"))
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, o.Status)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.Equal(t, order.Short, snap.Positions()[0].Side)
	assert.Equal(t, "1000", snap.Positions()[0].Quantity.String(), "the short must be completely unaffected")
}

// TestReduceOnly_BuyAgainstShortClosesCorrectly proves the Buy-side
// mirror of the baseline case.
func TestReduceOnly_BuyAgainstShortClosesCorrectly(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	openShortPosition(t, ctx, acc, deps.IDs, accountID, "1000")

	o, err := acc.Submit(ctx, mustReduceOnlyMarketRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Buy, "1000"))
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, o.Status)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions())
}

// TestReduceOnly_BuyAgainstLongNeverIncreasesLong is the Buy-side
// mirror of TestReduceOnly_SellAgainstShortNeverIncreasesShort.
func TestReduceOnly_BuyAgainstLongNeverIncreasesLong(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	openLongPosition(t, ctx, acc, deps.IDs, accountID, "1000")

	o, err := acc.Submit(ctx, mustReduceOnlyMarketRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Buy, "500"))
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, o.Status)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.Equal(t, "1000", snap.Positions()[0].Quantity.String())
}

// TestReduceOnly_OversizedCannotReversePosition proves the clamp
// (issue #352 review point 3): a ReduceOnly Sell requesting more than
// the held Long fills only up to the held quantity, closing to Flat
// rather than reversing into a Short.
func TestReduceOnly_OversizedCannotReversePosition(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	openLongPosition(t, ctx, acc, deps.IDs, accountID, "1000")

	o, err := acc.Submit(ctx, mustReduceOnlyMarketRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Sell, "1500"))
	require.NoError(t, err)
	assert.Equal(t, order.StatusPartiallyFilled, o.Status, "the unfillable remainder leaves the order partially filled, not silently dropped")
	assert.Equal(t, "1000", o.FilledQuantity.String())

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions(), "must close to Flat, never reverse into a Short")
}

// TestReduceOnly_StaleProtectiveStopCannotFillIntoFlat is issue #352's
// own exact reproduction: a resting ReduceOnly Stop (the protective
// stop) is still Working when a separate, independent ReduceOnly
// market order (the direct exit) closes the same position to Flat.
// When the resting Stop's trigger is then evaluated against a later
// Observation, it must be canceled instead of filling — never sell
// into a position that no longer exists and flip the account Short.
func TestReduceOnly_StaleProtectiveStopCannotFillIntoFlat(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	openLongPosition(t, ctx, acc, deps.IDs, accountID, "1000")

	// The protective stop: rests well below the market, never
	// canceled by anything else in this test.
	stopOrder, err := acc.Submit(ctx, mustReduceOnlyStopRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Sell, "1000", "1.05000"))
	require.NoError(t, err)
	require.Equal(t, order.StatusWorking, stopOrder.Status)

	// The independent direct exit: closes the position to Flat via a
	// completely separate ReduceOnly order.
	exitOrder, err := acc.Submit(ctx, mustReduceOnlyMarketRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Sell, "1000"))
	require.NoError(t, err)
	require.Equal(t, order.StatusFilled, exitOrder.Status)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Empty(t, snap.Positions(), "the direct exit must have already closed the position")

	// The observation's own Low breaches the stale stop's trigger
	// price — exactly what would normally fill it.
	obs := mustObservation(t, mustEurUsdListing(t), "1.09000", "1.09200", "1.04000", "1.08500", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err = acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions(), "the stale stop must never fill into a position that no longer exists")

	handle := acc.(*accountHandle)
	handle.state.mu.Lock()
	final := handle.state.orders[stopOrder.Request.OrderID]
	handle.state.mu.Unlock()
	assert.Equal(t, order.StatusCanceled, final.Status, "the stale protective stop must be canceled, not left dangling or filled")
}
