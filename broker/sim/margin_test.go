package sim

import (
	"testing"

	"github.com/rustyeddy/trader/market"
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
	meta := market.Instruments[instrument]

	units := 1000.0
	price := 1.2345
	quoteToAccount := 1.0

	got := TradeMargin(units, price, instrument, quoteToAccount)
	want := units * price * quoteToAccount * meta.MarginRate

	assert.InDelta(t, want, got, 1e-9)
}

func TestTradeMargin_NegativeUnits(t *testing.T) {
	t.Parallel()

	instrument := pickInstrument(t)
	meta := market.Instruments[instrument]

	units := -2500.0
	price := 2.0
	quoteToAccount := 0.9

	got := TradeMargin(units, price, instrument, quoteToAccount)
	want := (-units) * price * quoteToAccount * meta.MarginRate

	assert.InDelta(t, want, got, 1e-9)
}

func TestTradeMargin_ZeroUnits(t *testing.T) {
	t.Parallel()

	instrument := pickInstrument(t)

	got := TradeMargin(0, 1.5, instrument, 1.0)
	assert.InDelta(t, 0.0, got, 1e-12)
}
