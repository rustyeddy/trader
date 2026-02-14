package indicators

import (
	"testing"

	"github.com/rustyeddy/trader/pricing"
	"github.com/stretchr/testify/assert"
)

func TestExponentialMAStreaming(t *testing.T) {
	candles := []pricing.Candle{
		{C: 10}, {C: 11}, {C: 12}, {C: 13}, {C: 14}, {C: 15},
	}

	ema := NewEMA(3)
	for i, c := range candles {
		ema.Update(c)
		if i+1 < ema.Warmup() {
			assert.False(t, ema.Ready())
		}
	}
	assert.True(t, ema.Ready())
	assert.Greater(t, ema.Value(), 0.0)
}

func TestAverageTrueRangeStreaming(t *testing.T) {
	candles := []pricing.Candle{
		{H: 10, L: 8, C: 9},
		{H: 11, L: 9, C: 10},
		{H: 12, L: 10, C: 11},
		{H: 11, L: 9, C: 10},
		{H: 12, L: 10, C: 11},
		{H: 13, L: 11, C: 12},
	}

	atr := NewATR(3)
	assert.Equal(t, "ATR(3)", atr.Name())
	assert.Equal(t, 4, atr.Warmup())
	assert.False(t, atr.Ready())

	for _, c := range candles {
		atr.Update(c)
	}

	assert.True(t, atr.Ready())
	assert.InDelta(t, 2.0, atr.Value(), 0.001)
}

func TestADXStreaming(t *testing.T) {
	// Use an arbitrary but varying series.
	candles := []pricing.Candle{
		{H: 10, L: 8, C: 9},
		{H: 11, L: 9, C: 10},
		{H: 12, L: 10, C: 11},
		{H: 12, L: 9, C: 10},
		{H: 13, L: 10, C: 12},
		{H: 14, L: 11, C: 13},
		{H: 15, L: 12, C: 14},
		{H: 16, L: 13, C: 15},
		{H: 17, L: 14, C: 16},
		{H: 18, L: 15, C: 17},
		{H: 19, L: 16, C: 18},
		{H: 20, L: 17, C: 19},
		{H: 21, L: 18, C: 20},
		{H: 22, L: 19, C: 21},
		{H: 23, L: 20, C: 22},
		{H: 24, L: 21, C: 23},
		{H: 25, L: 22, C: 24},
		{H: 26, L: 23, C: 25},
		{H: 27, L: 24, C: 26},
		{H: 28, L: 25, C: 27},
		{H: 29, L: 26, C: 28},
		{H: 30, L: 27, C: 29},
		{H: 31, L: 28, C: 30},
		{H: 32, L: 29, C: 31},
		{H: 33, L: 30, C: 32},
		{H: 34, L: 31, C: 33},
		{H: 35, L: 32, C: 34},
		{H: 36, L: 33, C: 35},
		{H: 37, L: 34, C: 36},
		{H: 38, L: 35, C: 37},
	}

	adx := NewADX(5)
	for _, c := range candles {
		adx.Update(c)
	}
	assert.True(t, adx.Ready())
	assert.GreaterOrEqual(t, adx.Value(), 0.0)
	assert.LessOrEqual(t, adx.Value(), 100.0)
}

func TestIndicatorInterface(t *testing.T) {
	var _ Indicator = &SimpleMA{}
	var _ Indicator = &ExponentialMA{}
	var _ Indicator = &ATR{}
	var _ Indicator = &ADX{}
}
