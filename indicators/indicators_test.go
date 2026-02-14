package indicators

import (
	"testing"

	"github.com/rustyeddy/trader/pricing"
	"github.com/stretchr/testify/assert"
)

func createTestCandles() []pricing.Candle {
	// These are arbitrary integer (scaled) prices.
	return []pricing.Candle{
		{O: 100, H: 105, L: 99, C: 102},
		{O: 102, H: 107, L: 101, C: 105},
		{O: 105, H: 108, L: 104, C: 106},
		{O: 106, H: 110, L: 105, C: 108},
		{O: 108, H: 112, L: 107, C: 110},
		{O: 110, H: 113, L: 109, C: 111},
		{O: 111, H: 115, L: 110, C: 113},
		{O: 113, H: 116, L: 112, C: 114},
		{O: 114, H: 118, L: 113, C: 116},
		{O: 116, H: 120, L: 115, C: 118},
	}
}

func TestMA(t *testing.T) {
	candles := createTestCandles()

	ma, err := MA(candles, 5)
	assert.NoError(t, err)
	// Last 5 closes: 111,113,114,116,118 => 572/5 = 114.4
	assert.InDelta(t, 114.4, ma, 0.001)
}

func TestEMA(t *testing.T) {
	candles := createTestCandles()

	ema, err := EMA(candles, 5)
	assert.NoError(t, err)
	assert.Greater(t, ema, 0.0)
}

func TestATRFuncDetailed(t *testing.T) {
	candles := []pricing.Candle{
		{H: 10, L: 8, C: 9},
		{H: 11, L: 9, C: 10},
		{H: 12, L: 10, C: 11},
		{H: 11, L: 9, C: 10},
		{H: 12, L: 10, C: 11},
		{H: 13, L: 11, C: 12},
	}
	atr, err := ATRFunc(candles, 3)
	assert.NoError(t, err)
	assert.Equal(t, int32(2), atr)
}

func TestTrueRange(t *testing.T) {
	current := pricing.Candle{H: 110, L: 100, C: 105}
	previous := pricing.Candle{C: 104}
	tr := trueRange(current, previous)
	assert.Equal(t, int32(10), tr)
}
