package sim

import (
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

func pickInstrument(t *testing.T) string {
	t.Helper()
	for k := range market.Instruments {
		return k
	}
	t.Fatal("no instruments available")
	return ""
}

func TestTradeMargin_PositiveUnits(t *testing.T) {
	t.Parallel()

	instrument := pickInstrument(t)

	units := types.Units(1000)
	price := types.PriceFromFloat(1.2345)
	quoteToAccount := types.RateScale

	got, err := account.TradeMargin(units, price, instrument, types.Rate(quoteToAccount))
	assert.NoError(t, err)
	want, err := account.TradeMargin(units, price, instrument, types.Rate(quoteToAccount))
	assert.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestTradeMargin_NegativeUnits(t *testing.T) {
	t.Parallel()

	instrument := pickInstrument(t)

	units := types.Units(-2500)
	price := types.PriceFromFloat(2.0)
	quoteToAccount := types.RateFromFloat(0.9)

	got, err := account.TradeMargin(units, price, instrument, quoteToAccount)
	assert.NoError(t, err)
	want, err := account.TradeMargin(-units, price, instrument, quoteToAccount)
	assert.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestTradeMargin_ZeroUnits(t *testing.T) {
	t.Parallel()

	instrument := pickInstrument(t)

	got, err := account.TradeMargin(0, types.PriceFromFloat(1.5), instrument, types.RateScale)
	assert.NoError(t, err)
	assert.Equal(t, types.Money(0), got)
}
