package data

import (
	"testing"

	"github.com/stretchr/testify/assert"

	trader "github.com/rustyeddy/trader"
)

func TestNotionalUSD_USDQuoted(t *testing.T) {
	eurusd := trader.GetInstrument("EURUSD")
	// 100,000 units × 1.0845 = $108,450
	assert.InDelta(t, 108_450.0, notionalUSD(eurusd, 1.0845, 100_000), 0.01)
	assert.InDelta(t, 10_845.0, notionalUSD(eurusd, 1.0845, 10_000), 0.01)
	assert.InDelta(t, 1_084.5, notionalUSD(eurusd, 1.0845, 1_000), 0.01)
}

func TestNotionalUSD_USDBase(t *testing.T) {
	usdjpy := trader.GetInstrument("USDJPY")
	// USD is base, so notional = units regardless of price
	assert.InDelta(t, 100_000.0, notionalUSD(usdjpy, 150.0, 100_000), 0.01)
	assert.InDelta(t, 100_000.0, notionalUSD(usdjpy, 125.0, 100_000), 0.01)
}

func TestUnitsForNotional_USDQuoted(t *testing.T) {
	eurusd := trader.GetInstrument("EURUSD")
	// $10,845 / 1.0845 = 10,000 units
	assert.Equal(t, int64(10_000), unitsForNotional(eurusd, 1.0845, 10_845.0))
}

func TestUnitsForNotional_USDBase(t *testing.T) {
	usdjpy := trader.GetInstrument("USDJPY")
	// USD base: $50,000 notional = 50,000 units
	assert.Equal(t, int64(50_000), unitsForNotional(usdjpy, 150.0, 50_000.0))
}

func TestUnitsForNotional_ZeroPrice(t *testing.T) {
	eurusd := trader.GetInstrument("EURUSD")
	assert.Equal(t, int64(0), unitsForNotional(eurusd, 0, 10_000.0))
}

func TestFmtDollar(t *testing.T) {
	assert.Equal(t, "$108,450", fmtDollar(108_450.0))
	assert.Equal(t, "$1,000", fmtDollar(1000.0))
	assert.Equal(t, "$999.99", fmtDollar(999.99))
	assert.Equal(t, "$21.69", fmtDollar(21.69))
}

func TestPrintPositionTable_NoPanic(t *testing.T) {
	eurusd := trader.GetInstrument("EURUSD")
	assert.NotPanics(t, func() { printPositionTable(eurusd, 1.0845) })
}

func TestPrintSinglePosition_NoPanic(t *testing.T) {
	usdjpy := trader.GetInstrument("USDJPY")
	assert.NotPanics(t, func() { printSinglePosition(usdjpy, 150.0, 25_000) })
}

func TestPrintUnitsForNotional_NoPanic(t *testing.T) {
	gbpusd := trader.GetInstrument("GBPUSD")
	assert.NotPanics(t, func() { printUnitsForNotional(gbpusd, 1.2720, 5_000.0) })
}
