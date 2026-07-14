package review

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trendingCandles builds a monotonic uptrend: consistent directional bodies
// with small wicks, so ADX rises and CI stays low (trending regime).
func trendingCandles(n int) []market.Candle {
	candles := make([]market.Candle, 0, n)
	price := 1.10000
	for range n {
		open := price
		price += 0.00150
		close := price
		high := close + 0.00005
		low := open - 0.00005
		candles = append(candles, market.Candle{
			Open:  types.PriceFromFloat(open),
			High:  types.PriceFromFloat(high),
			Low:   types.PriceFromFloat(low),
			Close: types.PriceFromFloat(close),
		})
	}
	return candles
}

// rangingCandles builds a tight zigzag around a fixed level with wicks wider
// than the body, so ADX stays low and CI stays high (ranging regime).
func rangingCandles(n int) []market.Candle {
	candles := make([]market.Candle, 0, n)
	base := 1.10000
	for i := range n {
		var open, close float64
		if i%2 == 0 {
			open, close = base, base+0.00050
		} else {
			open, close = base+0.00050, base
		}
		high := open
		low := open
		if close > high {
			high = close
		}
		if close < low {
			low = close
		}
		high += 0.00100
		low -= 0.00100
		candles = append(candles, market.Candle{
			Open:  types.PriceFromFloat(open),
			High:  types.PriceFromFloat(high),
			Low:   types.PriceFromFloat(low),
			Close: types.PriceFromFloat(close),
		})
	}
	return candles
}

func TestReviewPair_UnknownInstrument(t *testing.T) {
	_, err := ReviewPair("NOTAPAIR", trendingCandles(40), trendingCandles(80), trendingCandles(80), DefaultThresholds())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown instrument")
}

func TestReviewPair_NotReady_InsufficientCandles(t *testing.T) {
	// Only 10 D1 candles — well short of the ADX/EMA(50) warmup requirement.
	_, err := ReviewPair("EURUSD", trendingCandles(40), trendingCandles(10), trendingCandles(80), DefaultThresholds())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient candles")
}

func TestReviewPair_Trending_ClassifiesHotOrTradeableLong(t *testing.T) {
	result, err := ReviewPair("EURUSD", trendingCandles(40), trendingCandles(80), trendingCandles(80), DefaultThresholds())
	require.NoError(t, err)

	assert.Equal(t, "EURUSD", result.Instrument)
	assert.Equal(t, "long", result.Bias)
	assert.Contains(t, []string{"hot", "tradeable"}, result.Bucket)
	assert.Greater(t, result.D1.ADX, 20.0)
	assert.Greater(t, result.D1.EMA20, result.D1.EMA50)
	assert.Greater(t, result.D1.TrendPct, 50.0)
}

func TestReviewPair_Ranging_ClassifiesWatch(t *testing.T) {
	result, err := ReviewPair("EURUSD", rangingCandles(40), rangingCandles(80), rangingCandles(80), DefaultThresholds())
	require.NoError(t, err)

	assert.Equal(t, "watch", result.Bucket)
	assert.Less(t, result.D1.ADX, 25.0)
}

func TestReviewPair_ATRPipsIsPositive(t *testing.T) {
	result, err := ReviewPair("EURUSD", trendingCandles(40), trendingCandles(80), trendingCandles(80), DefaultThresholds())
	require.NoError(t, err)
	assert.Greater(t, result.D1.ATRPips, 0.0)
	assert.Greater(t, result.H4.ATRPips, 0.0)
	assert.Greater(t, result.W1.ATRPips, 0.0)
}
