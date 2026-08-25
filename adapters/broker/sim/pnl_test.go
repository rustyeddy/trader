package sim

import (
	"context"
	"fmt"
	"testing"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mutablePriceSource is a FillPriceSource whose prices can change
// after construction — testDeps's fixedPriceSource is captured by
// value into Deps at NewBroker time, so reassigning a local Deps.Prices
// afterward has no effect on the already-constructed Broker; a test
// that needs the price to move between fills uses this instead.
type mutablePriceSource struct {
	prices map[string]num.Price
}

func (m *mutablePriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := m.prices[listing.Symbol()]
	if !ok {
		return num.Price{}, fmt.Errorf("mutablePriceSource: no price for %s", listing.Symbol())
	}
	return p, nil
}

func (m *mutablePriceSource) set(symbol string, price num.Price) {
	m.prices[symbol] = price
}

// TestSubmitClosesPositionExactlyAndRealizesPnL is an end-to-end
// (issue #152, M3-09) close scenario driven entirely through Submit.
func TestSubmitClosesPositionExactlyAndRealizesPnL(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	prices := &mutablePriceSource{prices: map[string]num.Price{"EUR_USD": num.MustParsePrice("1.10000")}}
	deps.Prices = prices
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	open := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, open)
	require.NoError(t, err)

	prices.set("EUR_USD", num.MustParsePrice("1.12000"))
	closeReq := mustMarketRequest(t, deps.IDs, accountID, order.Sell, "1000")
	_, err = acc.Submit(ctx, closeReq)
	require.NoError(t, err)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions(), "an exact close leaves no Position entry, flat or otherwise")
	assert.True(t, snap.RealizedPnL().Equal(usd("20")), "(1.12000-1.10000)*1000 = 20.00 USD")
	assert.True(t, snap.Equity().Equal(usd("10020")))
	assert.True(t, snap.CashBalances()[0].Equal(usd("10020")))
}

// TestAdvanceRevaluesPositionMarkWithoutAnyTrigger covers the
// requirement that unrealized PnL must not go stale merely because a
// bar had no pending order to trigger: a position with no working
// orders on its listing is still revalued from each Observation's
// Close.
func TestAdvanceRevaluesPositionMarkWithoutAnyTrigger(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	open := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000") // fills @ 1.10000
	_, err = acc.Submit(ctx, open)
	require.NoError(t, err)

	snapBefore, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.True(t, snapBefore.UnrealizedPnL().Equal(usd("0")))

	// No pending orders exist at all, so nothing can trigger; the bar
	// only carries new price information.
	obs := mustObservation(t, mustEurUsdListing(t), "1.10500", "1.10800", "1.10300", "1.10600", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snapAfter, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.True(t, snapAfter.UnrealizedPnL().Equal(usd("6")), "(1.10600-1.10000)*1000 = 6.00 USD, marked from the bar's Close")
	assert.True(t, snapAfter.Equity().Equal(usd("10006")))
	assert.True(t, snapAfter.RealizedPnL().Equal(usd("0")), "no fill occurred; nothing was realized")
	require.Len(t, snapAfter.Positions(), 1, "the position itself is unaffected, only its mark")
	assert.True(t, snapAfter.Positions()[0].Quantity.Equal(num.MustParseQuantity("1000")))
}

// TestAdvanceDoesNotRevalueWhenNoPositionExists confirms the mark
// revaluation step is a no-op — no state change, no error — when the
// account holds no position for the observed listing at all.
func TestAdvanceDoesNotRevalueWhenNoPositionExists(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	snapBefore, err := acc.Snapshot(ctx)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.10500", "1.10800", "1.10300", "1.10600", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snapAfter, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, snapBefore.AsOf(), snapAfter.AsOf(), "no state changed, so AsOf must not advance")
	assert.Empty(t, snapAfter.Positions())
}

// TestPositionPnLIsIsolatedAcrossAccounts confirms realized/unrealized
// PnL accounting, like cash and positions before it, never leaks
// between accounts.
func TestPositionPnLIsIsolatedAcrossAccounts(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	a1 := mustAccountID(t, deps.IDs)
	a2 := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps,
		AccountConfig{AccountID: a1, StartingCash: usd("10000")},
		AccountConfig{AccountID: a2, StartingCash: usd("10000")},
	)
	require.NoError(t, err)
	acc1, err := b.OpenAccount(ctx, a1)
	require.NoError(t, err)
	acc2, err := b.OpenAccount(ctx, a2)
	require.NoError(t, err)

	open := mustMarketRequestFor(t, deps.IDs, a1, mustEurUsdListing(t), order.Buy, "1000")
	_, err = acc1.Submit(ctx, open)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.10500", "1.10800", "1.10300", "1.10600", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap1, err := acc1.Snapshot(ctx)
	require.NoError(t, err)
	snap2, err := acc2.Snapshot(ctx)
	require.NoError(t, err)

	assert.False(t, snap1.UnrealizedPnL().IsZero())
	assert.True(t, snap2.UnrealizedPnL().Equal(usd("0")), "account 1's mark revaluation must not affect account 2")
	assert.Empty(t, snap2.Positions())
	assert.True(t, snap2.Equity().Equal(usd("10000")))
}
