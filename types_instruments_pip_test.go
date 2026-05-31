package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipValueUSD_USDQuoted(t *testing.T) {
	eurusd := GetInstrument("EURUSD")
	// 1 pip at 100,000 units = $10.00 exactly; rate is ignored
	assert.InDelta(t, 10.0, eurusd.PipValueUSD(0, 100_000, 1), 1e-9)
	assert.InDelta(t, 100.0, eurusd.PipValueUSD(0, 100_000, 10), 1e-9)
	assert.InDelta(t, 1000.0, eurusd.PipValueUSD(0, 100_000, 100), 1e-9)
	assert.InDelta(t, 10_000.0, eurusd.PipValueUSD(0, 100_000, 1000), 1e-9)
}

func TestPipValueUSD_USDBase(t *testing.T) {
	usdjpy := GetInstrument("USDJPY")
	// 1 pip (0.01 JPY) at 100,000 units at rate 150 → 0.01*100000/150 = $6.666...
	assert.InDelta(t, 6.6667, usdjpy.PipValueUSD(150.0, 100_000, 1), 1e-3)
	assert.InDelta(t, 66.667, usdjpy.PipValueUSD(150.0, 100_000, 10), 1e-2)
}

func TestPipValueUSD_ZeroRate(t *testing.T) {
	usdjpy := GetInstrument("USDJPY")
	assert.Equal(t, 0.0, usdjpy.PipValueUSD(0, 100_000, 1))
}

func TestPipValueUSD_MiniLot(t *testing.T) {
	gbpusd := GetInstrument("GBPUSD")
	// 10,000 units: 1 pip = $1.00
	assert.InDelta(t, 1.0, gbpusd.PipValueUSD(0, 10_000, 1), 1e-9)
}

func TestPipValueUSD_AllMajors(t *testing.T) {
	rates := map[string]float64{
		"USDJPY": 150.0,
		"USDCHF": 0.90,
		"USDCAD": 1.36,
	}
	for _, name := range Majors {
		inst := GetInstrument(name)
		rate := rates[name]
		v := inst.PipValueUSD(rate, 100_000, 1)
		assert.Greater(t, v, 0.0, "expected positive pip value for %s", name)
	}
}
