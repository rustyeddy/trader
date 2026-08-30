package order

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transitionUSD(t *testing.T, s string) num.Money {
	t.Helper()
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
}

func mustTransitionPosition(t *testing.T, side PositionSide, quantity, avgPrice string) Position {
	t.Helper()
	qty := num.MustParseQuantity(quantity)
	price := num.MustParsePrice(avgPrice)
	pos, err := NewPosition(Position{
		AccountID: mustAccountID(t),
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
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)

	result, err := ApplyFillToPosition(Position{}, false, accountID, listing, usdCode, Buy, num.MustParsePrice("1.10000"), num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, Long, result.Position.Side)
	assert.True(t, result.Position.Quantity.Equal(num.MustParseQuantity("1000")))
	require.NotNil(t, result.Position.AvgPrice)
	assert.True(t, result.Position.AvgPrice.Equal(num.MustParsePrice("1.10000")))
	assert.True(t, result.RealizedPnL.IsZero())
	assert.Equal(t, TransitionOpen, result.Transition)
}

func TestApplyFillToPositionOpensShortFromFlat(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)

	result, err := ApplyFillToPosition(Position{}, false, accountID, listing, usdCode, Sell, num.MustParsePrice("1.10000"), num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, Short, result.Position.Side)
	assert.True(t, result.RealizedPnL.IsZero())
	assert.Equal(t, TransitionOpen, result.Transition)
}

func TestApplyFillToPositionIncreasesLong(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)
	existing := mustTransitionPosition(t, Long, "1000", "1.10000")

	result, err := ApplyFillToPosition(existing, true, accountID, listing, usdCode, Buy, num.MustParsePrice("1.09500"), num.MustParseQuantity("500"))
	require.NoError(t, err)
	assert.Equal(t, Long, result.Position.Side)
	assert.True(t, result.Position.Quantity.Equal(num.MustParseQuantity("1500")))
	require.NotNil(t, result.Position.AvgPrice)
	assert.True(t, result.Position.AvgPrice.Equal(num.MustParsePrice("1.09833333")), "weighted average of (1.10000*1000 + 1.09500*500)/1500")
	assert.True(t, result.RealizedPnL.IsZero(), "increasing a position realizes no PnL")
	assert.Equal(t, TransitionIncrease, result.Transition)
}

func TestApplyFillToPositionIncreasesShort(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)
	existing := mustTransitionPosition(t, Short, "1000", "1.10000")

	result, err := ApplyFillToPosition(existing, true, accountID, listing, usdCode, Sell, num.MustParsePrice("1.10500"), num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, Short, result.Position.Side)
	assert.True(t, result.Position.Quantity.Equal(num.MustParseQuantity("2000")))
	require.NotNil(t, result.Position.AvgPrice)
	assert.True(t, result.Position.AvgPrice.Equal(num.MustParsePrice("1.10250")), "average of (1.10000+1.10500)/2 at equal weights")
	assert.True(t, result.RealizedPnL.IsZero())
	assert.Equal(t, TransitionIncrease, result.Transition)
}

func TestApplyFillToPositionReducesLongWithProfit(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)
	existing := mustTransitionPosition(t, Long, "1000", "1.10000")

	// Sell 400 at 1.11000: a profitable partial close for a Long.
	result, err := ApplyFillToPosition(existing, true, accountID, listing, usdCode, Sell, num.MustParsePrice("1.11000"), num.MustParseQuantity("400"))
	require.NoError(t, err)
	assert.Equal(t, Long, result.Position.Side)
	assert.True(t, result.Position.Quantity.Equal(num.MustParseQuantity("600")), "the remaining 600 stays open")
	require.NotNil(t, result.Position.AvgPrice)
	assert.True(t, result.Position.AvgPrice.Equal(num.MustParsePrice("1.10000")), "a reduce never changes the average price")
	assert.True(t, result.RealizedPnL.Equal(transitionUSD(t, "4")), "(1.11000-1.10000)*400 = 4.00 USD profit")
	assert.Equal(t, TransitionReduce, result.Transition)
}

func TestApplyFillToPositionReducesShortWithLoss(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)
	existing := mustTransitionPosition(t, Short, "1000", "1.10000")

	// Buy back 400 at 1.11000: a losing partial close for a Short
	// (bought back above the entry price).
	result, err := ApplyFillToPosition(existing, true, accountID, listing, usdCode, Buy, num.MustParsePrice("1.11000"), num.MustParseQuantity("400"))
	require.NoError(t, err)
	assert.Equal(t, Short, result.Position.Side)
	assert.True(t, result.Position.Quantity.Equal(num.MustParseQuantity("600")))
	wantLoss, err := transitionUSD(t, "4").Neg()
	require.NoError(t, err)
	assert.True(t, result.RealizedPnL.Equal(wantLoss), "(1.10000-1.11000)*400 = -4.00 USD loss")
	assert.Equal(t, TransitionReduce, result.Transition)
}

func TestApplyFillToPositionClosesExactly(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)
	existing := mustTransitionPosition(t, Long, "1000", "1.10000")

	result, err := ApplyFillToPosition(existing, true, accountID, listing, usdCode, Sell, num.MustParsePrice("1.12000"), num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, Flat, result.Position.Side)
	assert.True(t, result.Position.Quantity.IsZero())
	assert.Nil(t, result.Position.AvgPrice)
	assert.True(t, result.RealizedPnL.Equal(transitionUSD(t, "20")), "(1.12000-1.10000)*1000 = 20.00 USD profit")
	assert.Equal(t, TransitionClose, result.Transition)
}

func TestApplyFillToPositionReverses(t *testing.T) {
	usdCode := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)
	existing := mustTransitionPosition(t, Long, "1000", "1.10000")

	// Sell 1500 against a 1000-unit Long: closes the 1000 (realizing
	// PnL on exactly that much), then opens a fresh 500-unit Short at
	// the fill price.
	result, err := ApplyFillToPosition(existing, true, accountID, listing, usdCode, Sell, num.MustParsePrice("1.11000"), num.MustParseQuantity("1500"))
	require.NoError(t, err)
	assert.Equal(t, Short, result.Position.Side)
	assert.True(t, result.Position.Quantity.Equal(num.MustParseQuantity("500")))
	require.NotNil(t, result.Position.AvgPrice)
	assert.True(t, result.Position.AvgPrice.Equal(num.MustParsePrice("1.11000")), "the new Short opens at the fill price")
	assert.True(t, result.RealizedPnL.Equal(transitionUSD(t, "10")), "(1.11000-1.10000)*1000 = 10.00 USD profit on the closed portion only")
	assert.Equal(t, TransitionReverse, result.Transition)
}
