package service

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
)

func TestPosUnitsForNotional_USDBase(t *testing.T) {
	// USD/JPY — base is USD; units == targetUSD regardless of price.
	inst := market.GetInstrument("USDJPY")
	assert.Equal(t, int64(1000), posUnitsForNotional(inst, market.PriceFromFloat(150.0), 1000.0))
	assert.Equal(t, int64(501), posUnitsForNotional(inst, market.PriceFromFloat(150.0), 500.5)) // math.Round rounds half away from zero
}

func TestPosUnitsForNotional_NonUSDBase(t *testing.T) {
	// EUR/USD — 10 000 USD / 1.10 price ≈ 9 091 units.
	inst := market.GetInstrument("EURUSD")
	got := posUnitsForNotional(inst, market.PriceFromFloat(1.10), 10_000.0)
	assert.InDelta(t, int64(9091), got, 1)
}

func TestPosUnitsForNotional_ZeroPrice(t *testing.T) {
	inst := market.GetInstrument("EURUSD")
	assert.Equal(t, int64(0), posUnitsForNotional(inst, 0, 1000.0))
}

func TestPosNotionalUSD_USDBase(t *testing.T) {
	inst := market.GetInstrument("USDJPY")
	assert.InDelta(t, 10_000.0, posNotionalUSD(inst, 150.0, 10_000), 1e-9)
}

func TestPosNotionalUSD_NonUSDBase(t *testing.T) {
	inst := market.GetInstrument("EURUSD")
	assert.InDelta(t, 11_000.0, posNotionalUSD(inst, 1.10, 10_000), 1e-9)
}
