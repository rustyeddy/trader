package trader

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChoppinessIndex_ErrorOnBadArgs(t *testing.T) {
	_, err := NewChoppinessIndex(1, 100000)
	assert.Error(t, err)
	_, err = NewChoppinessIndex(14, 0)
	assert.Error(t, err)
}

func TestChoppinessIndex_NotReadyBeforeWarmup(t *testing.T) {
	ci, err := NewChoppinessIndex(5, 100000)
	require.NoError(t, err)
	c := Candle{Open: 110000, High: 111000, Low: 109000, Close: 110500}
	for i := 0; i < 4; i++ {
		ci.Update(c)
		assert.False(t, ci.Ready())
	}
	ci.Update(c)
	assert.True(t, ci.Ready())
}

func TestChoppinessIndex_PerfectChop(t *testing.T) {
	// Identical candles: sum(TR) = N × range; HH−LL = range → CI = 100.
	ci, err := NewChoppinessIndex(5, 100000)
	require.NoError(t, err)
	c := Candle{Open: 105000, High: 110000, Low: 100000, Close: 105000}
	for i := 0; i < 5; i++ {
		ci.Update(c)
	}
	require.True(t, ci.Ready())
	assert.InDelta(t, 100.0, ci.Float64(), 0.001)
}

func TestChoppinessIndex_PerfectTrend(t *testing.T) {
	// Non-overlapping bars [0,1], [1,2], ... [4,5] (in price units × scale).
	// sum(TR) = 5; HH−LL = 5 → CI = 100×log10(1)/log10(5) = 0.
	ci, err := NewChoppinessIndex(5, 100000)
	require.NoError(t, err)
	scale := 100000.0
	for i := 0; i < 5; i++ {
		lo := Price(float64(i) * scale)
		hi := Price(float64(i+1) * scale)
		ci.Update(Candle{Open: lo, High: hi, Low: lo, Close: hi})
	}
	require.True(t, ci.Ready())
	assert.Less(t, ci.Float64(), 5.0, "expected CI near 0 for perfect trend, got %.2f", ci.Float64())
}

func TestChoppinessIndex_Reset(t *testing.T) {
	ci, err := NewChoppinessIndex(5, 100000)
	require.NoError(t, err)
	c := Candle{Open: 105000, High: 110000, Low: 100000, Close: 105000}
	for i := 0; i < 5; i++ {
		ci.Update(c)
	}
	require.True(t, ci.Ready())
	ci.Reset()
	assert.False(t, ci.Ready())
	assert.Equal(t, 0.0, ci.Float64())
}

func TestChoppinessIndex_ValueInRange(t *testing.T) {
	// Realistic trending sequence: CI should be between 0 and 100.
	ci, err := NewChoppinessIndex(14, 100000)
	require.NoError(t, err)
	prices := []float64{
		1.0800, 1.0820, 1.0810, 1.0830, 1.0815,
		1.0850, 1.0840, 1.0860, 1.0855, 1.0870,
		1.0865, 1.0880, 1.0875, 1.0890, 1.0885, 1.0900,
	}
	for i := 1; i < len(prices); i++ {
		lo := math.Min(prices[i-1], prices[i]) - 0.0005
		hi := math.Max(prices[i-1], prices[i]) + 0.0005
		mid := (prices[i-1] + prices[i]) / 2
		ci.Update(Candle{
			Open:  Price(mid * 100000),
			High:  Price(hi * 100000),
			Low:   Price(lo * 100000),
			Close: Price(prices[i] * 100000),
		})
	}
	require.True(t, ci.Ready())
	assert.Greater(t, ci.Float64(), 0.0)
	assert.Less(t, ci.Float64(), 100.0)
}
