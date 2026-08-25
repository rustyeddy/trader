package sim

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustPosition(t *testing.T, side order.PositionSide, quantity, avgPrice string) order.Position {
	t.Helper()
	qty := num.MustParseQuantity(quantity)
	price := num.MustParsePrice(avgPrice)
	pos, err := order.NewPosition(order.Position{
		AccountID: mustAccountID(t, testDeps().IDs),
		Listing:   mustEurUsdListing(t),
		Side:      side,
		Quantity:  qty,
		AvgPrice:  &price,
	})
	require.NoError(t, err)
	return pos
}

func TestApplyFillToPositionOpensFromFlat(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t, testDeps().IDs)
	listing := mustEurUsdListing(t)

	pos, pnl, err := applyFillToPosition(order.Position{}, false, accountID, listing, usdCode, order.Buy, num.MustParsePrice("1.10000"), num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, order.Long, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("1000")))
	require.NotNil(t, pos.AvgPrice)
	assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.10000")))
	assert.True(t, pnl.IsZero())
}

func TestApplyFillToPositionOpensShortFromFlat(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t, testDeps().IDs)
	listing := mustEurUsdListing(t)

	pos, pnl, err := applyFillToPosition(order.Position{}, false, accountID, listing, usdCode, order.Sell, num.MustParsePrice("1.10000"), num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, order.Short, pos.Side)
	assert.True(t, pnl.IsZero())
}

func TestApplyFillToPositionIncreasesLong(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t, testDeps().IDs)
	listing := mustEurUsdListing(t)
	existing := mustPosition(t, order.Long, "1000", "1.10000")

	pos, pnl, err := applyFillToPosition(existing, true, accountID, listing, usdCode, order.Buy, num.MustParsePrice("1.09500"), num.MustParseQuantity("500"))
	require.NoError(t, err)
	assert.Equal(t, order.Long, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("1500")))
	require.NotNil(t, pos.AvgPrice)
	assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.09833333")), "weighted average of (1.10000*1000 + 1.09500*500)/1500")
	assert.True(t, pnl.IsZero(), "increasing a position realizes no PnL")
}

func TestApplyFillToPositionIncreasesShort(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t, testDeps().IDs)
	listing := mustEurUsdListing(t)
	existing := mustPosition(t, order.Short, "1000", "1.10000")

	pos, pnl, err := applyFillToPosition(existing, true, accountID, listing, usdCode, order.Sell, num.MustParsePrice("1.10500"), num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, order.Short, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("2000")))
	require.NotNil(t, pos.AvgPrice)
	assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.10250")), "average of (1.10000+1.10500)/2 at equal weights")
	assert.True(t, pnl.IsZero())
}

func TestApplyFillToPositionReducesLongWithProfit(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t, testDeps().IDs)
	listing := mustEurUsdListing(t)
	existing := mustPosition(t, order.Long, "1000", "1.10000")

	// Sell 400 at 1.11000: a profitable partial close for a Long.
	pos, pnl, err := applyFillToPosition(existing, true, accountID, listing, usdCode, order.Sell, num.MustParsePrice("1.11000"), num.MustParseQuantity("400"))
	require.NoError(t, err)
	assert.Equal(t, order.Long, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("600")), "the remaining 600 stays open")
	require.NotNil(t, pos.AvgPrice)
	assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.10000")), "a reduce never changes the average price")
	assert.True(t, pnl.Equal(usd("4")), "(1.11000-1.10000)*400 = 4.00 USD profit")
}

func TestApplyFillToPositionReducesShortWithLoss(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t, testDeps().IDs)
	listing := mustEurUsdListing(t)
	existing := mustPosition(t, order.Short, "1000", "1.10000")

	// Buy back 400 at 1.11000: a losing partial close for a Short
	// (bought back above the entry price).
	pos, pnl, err := applyFillToPosition(existing, true, accountID, listing, usdCode, order.Buy, num.MustParsePrice("1.11000"), num.MustParseQuantity("400"))
	require.NoError(t, err)
	assert.Equal(t, order.Short, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("600")))
	wantLoss, err := usd("4").Neg()
	require.NoError(t, err)
	assert.True(t, pnl.Equal(wantLoss), "(1.10000-1.11000)*400 = -4.00 USD loss")
}

func TestApplyFillToPositionClosesExactly(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t, testDeps().IDs)
	listing := mustEurUsdListing(t)
	existing := mustPosition(t, order.Long, "1000", "1.10000")

	pos, pnl, err := applyFillToPosition(existing, true, accountID, listing, usdCode, order.Sell, num.MustParsePrice("1.12000"), num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, order.Flat, pos.Side)
	assert.True(t, pos.Quantity.IsZero())
	assert.Nil(t, pos.AvgPrice)
	assert.True(t, pnl.Equal(usd("20")), "(1.12000-1.10000)*1000 = 20.00 USD profit")
}

func TestApplyFillToPositionReverses(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t, testDeps().IDs)
	listing := mustEurUsdListing(t)
	existing := mustPosition(t, order.Long, "1000", "1.10000")

	// Sell 1500 against a 1000-unit Long: closes the 1000 (realizing
	// PnL on exactly that much), then opens a fresh 500-unit Short at
	// the fill price.
	pos, pnl, err := applyFillToPosition(existing, true, accountID, listing, usdCode, order.Sell, num.MustParsePrice("1.11000"), num.MustParseQuantity("1500"))
	require.NoError(t, err)
	assert.Equal(t, order.Short, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("500")))
	require.NotNil(t, pos.AvgPrice)
	assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.11000")), "the new Short opens at the fill price")
	assert.True(t, pnl.Equal(usd("10")), "(1.11000-1.10000)*1000 = 10.00 USD profit on the closed portion only")
}

func TestApplyCommissionDebitsCashAndAccumulatesFees(t *testing.T) {
	cash := usd("10000")
	fees := usd("0")
	commission := usd("2.50")

	newCash, newFees, err := applyCommission(cash, fees, commission)
	require.NoError(t, err)
	assert.True(t, newCash.Equal(usd("9997.50")))
	assert.True(t, newFees.Equal(usd("2.50")))
}

func TestApplyCommissionAccumulatesAcrossCalls(t *testing.T) {
	cash := usd("10000")
	fees := usd("2.50")
	commission := usd("1.50")

	newCash, newFees, err := applyCommission(cash, fees, commission)
	require.NoError(t, err)
	assert.True(t, newCash.Equal(usd("9998.50")))
	assert.True(t, newFees.Equal(usd("4")))
}

func TestApplyCommissionRejectsCurrencyMismatch(t *testing.T) {
	cash := usd("10000")
	fees := usd("0")
	commission := num.MustParseMoney("2.50", num.MustParseCurrency("EUR"))

	_, _, err := applyCommission(cash, fees, commission)
	require.ErrorIs(t, err, num.ErrCurrencyMismatch)
}

func TestUnrealizedPnLForPositionLong(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	pos := mustPosition(t, order.Long, "1000", "1.10000")

	profit, err := unrealizedPnLForPosition(pos, num.MustParsePrice("1.10500"), usdCode)
	require.NoError(t, err)
	assert.True(t, profit.Equal(usd("5")))

	loss, err := unrealizedPnLForPosition(pos, num.MustParsePrice("1.09500"), usdCode)
	require.NoError(t, err)
	wantLoss, err := usd("5").Neg()
	require.NoError(t, err)
	assert.True(t, loss.Equal(wantLoss))
}

func TestUnrealizedPnLForPositionShort(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	pos := mustPosition(t, order.Short, "1000", "1.10000")

	profit, err := unrealizedPnLForPosition(pos, num.MustParsePrice("1.09500"), usdCode)
	require.NoError(t, err)
	assert.True(t, profit.Equal(usd("5")), "a short profits when the mark falls below the average")
}
