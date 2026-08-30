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
