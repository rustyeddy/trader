package trader

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradeMargin_Basic(t *testing.T) {
	t.Parallel()

	units := types.Units(10_000)
	price := types.PriceFromFloat(1.1000)
	rate := types.Rate(types.RateScale)

	got, err := TradeMargin(units, price, "EURUSD", rate)
	require.NoError(t, err)
	assert.Greater(t, int64(got), int64(0))
}

func TestTradeMargin_NegativeUnitsSymmetric(t *testing.T) {
	t.Parallel()

	price := types.PriceFromFloat(1.2345)
	rate := types.Rate(types.RateScale)

	pos, err := TradeMargin(types.Units(1000), price, "EURUSD", rate)
	require.NoError(t, err)
	neg, err := TradeMargin(types.Units(-1000), price, "EURUSD", rate)
	require.NoError(t, err)

	assert.Equal(t, pos, neg, "margin for ±units must be equal")
}

func TestTradeMargin_ZeroUnits(t *testing.T) {
	t.Parallel()

	got, err := TradeMargin(0, types.PriceFromFloat(1.5), "EURUSD", types.Rate(types.RateScale))
	require.NoError(t, err)
	assert.Equal(t, types.Money(0), got)
}

func TestTradeMargin_InvalidRate(t *testing.T) {
	t.Parallel()

	_, err := TradeMargin(1000, types.PriceFromFloat(1.1), "EURUSD", 0)
	assert.Error(t, err)
}

func TestTradeMargin_InvalidPrice(t *testing.T) {
	t.Parallel()

	_, err := TradeMargin(1000, 0, "EURUSD", types.Rate(types.RateScale))
	assert.Error(t, err)
}

func TestTradeMargin_UnknownInstrument(t *testing.T) {
	t.Parallel()

	_, err := TradeMargin(1000, types.PriceFromFloat(1.1), "XXXYYY", types.Rate(types.RateScale))
	assert.Error(t, err)
}
