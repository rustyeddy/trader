// pkg/indicators/adx_test.go
package indicators

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/require"
)

func mkCandle(scale int32, o, h, l, c float64) market.Candle {
	toP := func(x float64) market.Price { return market.Price(x*float64(scale) + 0.5) }
	return market.Candle{
		O: toP(o),
		H: toP(h),
		L: toP(l),
		C: toP(c),
	}
}

func feedFlat(t *testing.T, adx *ADX, scale int32, nCandles int, price float64) {
	for i := 0; i < nCandles; i++ {
		// completely flat OHLC => TR=0, +DM/-DM=0 => DI/DX/ADX should go to 0
		adx.Update(mkCandle(scale, price, price, price, price))
	}
}

func feedUptrend(t *testing.T, adx *ADX, scale int32, nCandles int, start float64, step float64, halfRange float64) {
	p := start
	for i := 0; i < nCandles; i++ {
		o := p
		c := p + step
		h := c + halfRange
		l := o - halfRange
		adx.Update(mkCandle(scale, o, h, l, c))
		p = c
	}
}

func TestADX_WarmupAndReady(t *testing.T) {
	scale := int32(10000)
	n := 14
	adx := NewADX(n, scale)

	require.False(t, adx.Ready())
	require.Equal(t, 2*n, adx.Warmup())
	require.Equal(t, 0.0, adx.Float64())

	// Feed more than enough candles to ensure readiness.
	// (Needs prev candle + ~2N periods; using 3N candles is comfortably over.)
	feedUptrend(t, adx, scale, 3*n, 1.0000, 0.0001, 0.00005)

	require.True(t, adx.Ready(), "ADX should be ready after sufficient candles")
	require.GreaterOrEqual(t, adx.Float64(), 0.0)
	require.LessOrEqual(t, adx.Float64(), 100.0)
}

func TestADX_FlatMarketGoesToZero(t *testing.T) {
	scale := int32(10000)
	n := 14
	adx := NewADX(n, scale)

	// Plenty of candles so it becomes ready
	feedFlat(t, adx, scale, 3*n, 1.2345)

	require.True(t, adx.Ready())
	require.InDelta(t, 0.0, adx.PlusDI(), 1e-12)
	require.InDelta(t, 0.0, adx.MinusDI(), 1e-12)
	require.InDelta(t, 0.0, adx.DX(), 1e-12)
	require.InDelta(t, 0.0, adx.Float64(), 1e-12)
}

func TestADX_UptrendHasPlusDIOverMinusDIAndPositiveADX(t *testing.T) {
	scale := int32(10000)
	n := 14
	adx := NewADX(n, scale)

	// Build a steady uptrend: close increases 1 pip each candle, with small range.
	feedUptrend(t, adx, scale, 3*n, 1.0000, 0.0001, 0.00005)

	require.True(t, adx.Ready())
	require.Greater(t, adx.PlusDI(), adx.MinusDI(), "+DI should exceed -DI in an uptrend")

	// ADX should be > 0 in a trending series (not asserting an exact value).
	require.Greater(t, adx.Float64(), 0.0)
	require.LessOrEqual(t, adx.Float64(), 100.0)
}

func TestADX_Reset(t *testing.T) {
	scale := int32(10000)
	n := 14
	adx := NewADX(n, scale)

	feedUptrend(t, adx, scale, 3*n, 1.0000, 0.0001, 0.00005)
	require.True(t, adx.Ready())
	require.Greater(t, adx.Float64(), 0.0)

	adx.Reset()
	require.False(t, adx.Ready())
	require.Equal(t, 0.0, adx.Float64())
	require.Equal(t, 0.0, adx.PlusDI())
	require.Equal(t, 0.0, adx.MinusDI())
}
