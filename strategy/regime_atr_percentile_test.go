package strategy

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// atrPCT builds a candle with the given OHLC values (all prices scaled).
func atrPCT(open, high, low, close types.Price) market.Candle {
	return market.Candle{Open: open, High: high, Low: low, Close: close}
}

func TestATRPercentileFilter_NotReadyBeforeWarmup(t *testing.T) {
	t.Parallel()
	f, err := NewATRPercentileFilter(3, 10, 20.0, types.PriceScale)
	require.NoError(t, err)
	assert.False(t, f.Ready())
	// During warmup, Trending and AllowSide must not gate entries.
	assert.True(t, f.Trending())
	assert.True(t, f.AllowSide(types.Long))
	assert.True(t, f.AllowSide(types.Short))
}

func TestATRPercentileFilter_ReadyAfterWarmup(t *testing.T) {
	t.Parallel()
	// ATR(3) needs 4 candles (3 periods = 4 candles). windowSize=5 needs 1 ATR value.
	f, err := NewATRPercentileFilter(3, 5, 20.0, types.PriceScale)
	require.NoError(t, err)

	candles := []market.Candle{
		atrPCT(100000, 101000, 99000, 100500),
		atrPCT(100500, 101500, 99500, 101000),
		atrPCT(101000, 102000, 100000, 101500),
		atrPCT(101500, 102500, 100500, 102000),
	}
	for _, ct := range candles {
		f.Tick(ct)
	}
	assert.True(t, f.Ready())
}

func TestATRPercentileFilter_ReadyBeforeWindowIsFull(t *testing.T) {
	t.Parallel()

	f, err := NewATRPercentileFilter(3, 50, 20.0, types.PriceScale)
	require.NoError(t, err)

	candles := []market.Candle{
		atrPCT(100000, 101000, 99000, 100500),
		atrPCT(100500, 101500, 99500, 101000),
		atrPCT(101000, 102000, 100000, 101500),
		atrPCT(101500, 102500, 100500, 102000),
	}
	for _, ct := range candles {
		f.Tick(ct)
	}

	require.True(t, f.Ready())
	assert.Equal(t, 1, f.count, "the filter should start classifying after the first ATR sample")
}

func TestATRPercentileFilter_AllowSideAlwaysTrue(t *testing.T) {
	t.Parallel()
	f, err := NewATRPercentileFilter(3, 10, 20.0, types.PriceScale)
	require.NoError(t, err)
	// Feed enough bars to warm up.
	for i := 0; i < 20; i++ {
		p := types.Price(100000 + i*1000)
		f.Tick(atrPCT(p, p+500, p-500, p+200))
	}
	require.True(t, f.Ready())
	assert.True(t, f.AllowSide(types.Long), "ATRPercentile.AllowSide must always return true")
	assert.True(t, f.AllowSide(types.Short), "ATRPercentile.AllowSide must always return true")
}

func TestATRPercentileFilter_TrendingTrueWhenHighVolatility(t *testing.T) {
	t.Parallel()
	// Feed a long sequence of normal-range bars, then a burst of wide bars.
	// After the burst the current ATR should rank above the 20th percentile.
	f, err := NewATRPercentileFilter(5, 50, 20.0, types.PriceScale)
	require.NoError(t, err)

	// 50 narrow bars to fill the window
	for i := 0; i < 50; i++ {
		p := types.Price(100000 + i*100)
		f.Tick(atrPCT(p, p+100, p-100, p+50))
	}
	// 10 wide bars to push ATR up
	for i := 0; i < 10; i++ {
		p := types.Price(105000 + i*100)
		f.Tick(atrPCT(p, p+3000, p-3000, p+1000))
	}
	require.True(t, f.Ready())
	assert.True(t, f.Trending(), "high ATR percentile must return Trending=true")
}

func TestATRPercentileFilter_TrendingFalseWhenLowVolatility(t *testing.T) {
	t.Parallel()
	// Fill window with wide bars, then switch to very narrow bars.
	// ATR will drop and rank below the 20th percentile.
	f, err := NewATRPercentileFilter(5, 50, 20.0, types.PriceScale)
	require.NoError(t, err)

	// 50 wide bars
	for i := 0; i < 50; i++ {
		p := types.Price(100000 + i*100)
		f.Tick(atrPCT(p, p+3000, p-3000, p+1000))
	}
	// 10 very narrow bars to collapse ATR
	for i := 0; i < 10; i++ {
		p := types.Price(105000 + i*10)
		f.Tick(atrPCT(p, p+5, p-5, p+2))
	}
	require.True(t, f.Ready())
	assert.False(t, f.Trending(), "low ATR percentile must return Trending=false")
}

func TestATRPercentileFilter_FlatWindowUsesTieAwarePercentile(t *testing.T) {
	t.Parallel()

	f, err := NewATRPercentileFilter(1, 5, 20.0, types.PriceScale)
	require.NoError(t, err)

	for i := 0; i < 7; i++ {
		f.Tick(atrPCT(100000, 101000, 99000, 100000))
	}

	require.True(t, f.Ready())
	assert.Equal(t, 50.0, f.Percentile())
	assert.True(t, f.Trending(), "flat ATR history should not collapse to the 0th percentile")
}

func TestATRPercentileFilter_RejectsInvalidThreshold(t *testing.T) {
	t.Parallel()

	_, err := NewATRPercentileFilter(5, 50, -0.1, types.PriceScale)
	require.Error(t, err)

	_, err = NewATRPercentileFilter(5, 50, 100.1, types.PriceScale)
	require.Error(t, err)
}

func TestATRPercentileFilter_FactoryRoundtrip(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{
		Kind: "atr-percentile",
		Params: map[string]any{
			"atr_period":  int32(10),
			"window_size": int32(100),
			"threshold":   30.0,
		},
	}, types.PriceScale)
	require.NoError(t, err)
	af, ok := f.(*ATRPercentileFilter)
	require.True(t, ok)
	assert.Equal(t, 10, af.atrPeriod)
	assert.Equal(t, 100, af.windowSize)
	assert.Equal(t, 30.0, af.threshold)
}

func TestATRPercentileFilter_FactoryDefaults(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{Kind: "atr-percentile"}, types.PriceScale)
	require.NoError(t, err)
	af, ok := f.(*ATRPercentileFilter)
	require.True(t, ok)
	assert.Equal(t, 20, af.atrPeriod)
	assert.Equal(t, 200, af.windowSize)
	assert.Equal(t, 20.0, af.threshold)
}

func TestATRPercentileFilter_Name(t *testing.T) {
	t.Parallel()
	f, err := NewATRPercentileFilter(20, 200, 20.0, types.PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "ATRPercentile(20,200,20)", f.Name())
}
