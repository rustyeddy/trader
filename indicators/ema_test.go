package indicators

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// helper to create a candle with close only
func candle(close float64) types.Candle {
	return types.Candle{
		Close: types.Price(close * float64(types.PriceScale)),
	}
}

func TestEMA_WarmupAndReady(t *testing.T) {
	ema := NewEMA(3, types.PriceScale)

	require.False(t, ema.Ready())
	require.Equal(t, 3, ema.Warmup())

	ema.Update(candle(1.0))
	require.False(t, ema.Ready())

	ema.Update(candle(2.0))
	require.False(t, ema.Ready())

	ema.Update(candle(3.0))
	require.True(t, ema.Ready())
}

func TestEMA_KnownSequence(t *testing.T) {
	ema := NewEMA(3, types.PriceScale)

	// period = 3
	// alpha = 2/(3+1) = 0.5
	//
	// sequence: 10, 11, 12, 13
	//
	// EMA steps:
	// 1) seed = 10
	// 2) 0.5*11 + 0.5*10 = 10.5
	// 3) 0.5*12 + 0.5*10.5 = 11.25
	// 4) 0.5*13 + 0.5*11.25 = 12.125

	values := []float64{10, 11, 12, 13}

	var result float64
	for _, v := range values {
		ema.Update(candle(v))
		result = ema.Float64()
	}

	require.True(t, ema.Ready())
	require.InDelta(t, 12.125, result, 1e-9)
}

func TestEMA_Reset(t *testing.T) {
	ema := NewEMA(3, types.PriceScale)

	ema.Update(candle(10))
	ema.Update(candle(11))
	require.False(t, ema.Ready())

	ema.Reset()

	require.False(t, ema.Ready())
	require.Equal(t, 0.0, ema.Float64())

	ema.Update(candle(20))
	require.Equal(t, 20.0, ema.Float64())
}
