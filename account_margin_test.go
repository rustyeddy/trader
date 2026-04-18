package trader

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTradeMargin_Basic(t *testing.T) {
	t.Parallel()

	units := Units(10_000)
	price := PriceFromFloat(1.1000)
	rate := Rate(RateScale)

	got, err := TradeMargin(units, price, "EURUSD", rate)
	require.NoError(t, err)
	assert.Greater(t, int64(got), int64(0))
}

func TestTradeMargin_NegativeUnitsSymmetric(t *testing.T) {
	t.Parallel()

	price := PriceFromFloat(1.2345)
	rate := Rate(RateScale)

	pos, err := TradeMargin(Units(1000), price, "EURUSD", rate)
	require.NoError(t, err)
	neg, err := TradeMargin(Units(-1000), price, "EURUSD", rate)
	require.NoError(t, err)

	assert.Equal(t, pos, neg, "margin for ±units must be equal")
}

func TestTradeMargin_ZeroUnits(t *testing.T) {
	t.Parallel()

	got, err := TradeMargin(0, PriceFromFloat(1.5), "EURUSD", Rate(RateScale))
	require.NoError(t, err)
	assert.Equal(t, Money(0), got)
}

func TestTradeMargin_InvalidRate(t *testing.T) {
	t.Parallel()

	_, err := TradeMargin(1000, PriceFromFloat(1.1), "EURUSD", 0)
	assert.Error(t, err)
}

func TestTradeMargin_InvalidPrice(t *testing.T) {
	t.Parallel()

	_, err := TradeMargin(1000, 0, "EURUSD", Rate(RateScale))
	assert.Error(t, err)
}

func TestTradeMargin_UnknownInstrument(t *testing.T) {
	t.Parallel()

	_, err := TradeMargin(1000, PriceFromFloat(1.1), "XXXYYY", Rate(RateScale))
	assert.Error(t, err)
}
