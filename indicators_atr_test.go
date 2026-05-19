package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestATR_PanicGuards(t *testing.T) {
	require.Panics(t, func() { NewATR(0, PriceScale) })
	require.Panics(t, func() { NewATR(3, 0) })
}

func TestATR_WarmupAndReady(t *testing.T) {
	a := NewATR(3, PriceScale)
	assert.Equal(t, "ATR(3)", a.Name())
	assert.Equal(t, 4, a.Warmup()) // period+1
	assert.False(t, a.Ready())
	assert.Equal(t, 0.0, a.Float64())

	// Feed N+1 candles: ready after exactly N periods (N+1 candles).
	for i := 0; i < 3; i++ {
		a.Update(mkCandle(int32(PriceScale), 1.0, 1.01, 0.99, 1.0))
		assert.False(t, a.Ready(), "should not be ready after candle %d", i+1)
	}
	a.Update(mkCandle(int32(PriceScale), 1.0, 1.01, 0.99, 1.0))
	assert.True(t, a.Ready())
	assert.Greater(t, a.Float64(), 0.0)
}

func TestATR_Reset(t *testing.T) {
	a := NewATR(3, PriceScale)
	for i := 0; i < 5; i++ {
		a.Update(mkCandle(int32(PriceScale), 1.0, 1.01, 0.99, 1.0))
	}
	require.True(t, a.Ready())

	a.Reset()
	assert.False(t, a.Ready())
	assert.Equal(t, 0.0, a.Float64())
}

func TestATR_ValueIncreases_WithVolatility(t *testing.T) {
	a := NewATR(5, PriceScale)

	// seed with low-volatility candles
	for i := 0; i < 10; i++ {
		a.Update(mkCandle(int32(PriceScale), 1.0, 1.001, 0.999, 1.0))
	}
	require.True(t, a.Ready())
	lowATR := a.Float64()

	// feed high-volatility candles
	for i := 0; i < 10; i++ {
		a.Update(mkCandle(int32(PriceScale), 1.0, 1.02, 0.98, 1.0))
	}
	highATR := a.Float64()

	assert.Greater(t, highATR, lowATR, "ATR should rise with increased volatility")
}
