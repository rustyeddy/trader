package candlepattern

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func px(f float64) types.Price { return types.PriceFromFloat(f) }

func quietCandle(close float64) market.Candle {
	return market.Candle{Open: px(close), High: px(close + 0.5), Low: px(close - 0.5), Close: px(close)}
}

// bullishRejectionCandle has a long lower wick (rejecting lower prices)
// with the close near the high — the classic bullish pin bar.
func bullishRejectionCandle() market.Candle {
	return market.Candle{Open: px(100), High: px(101), Low: px(90), Close: px(100.5)}
}

// bearishRejectionCandle has a long upper wick with the close near the low.
func bearishRejectionCandle() market.Candle {
	return market.Candle{Open: px(100), High: px(110), Low: px(99), Close: px(99.5)}
}

func newTestWickRejection(t *testing.T) *WickRejection {
	t.Helper()
	w, err := NewWickRejection(0.5, 0.3, 0.5, 1, 2, types.PriceScale)
	require.NoError(t, err)
	return w
}

func TestNewWickRejection_ValidationErrors(t *testing.T) {
	t.Parallel()
	_, err := NewWickRejection(0, 0.3, 0.5, 1, 2, types.PriceScale)
	assert.ErrorContains(t, err, "min-wick-ratio")

	_, err = NewWickRejection(1.5, 0.3, 0.5, 1, 2, types.PriceScale)
	assert.ErrorContains(t, err, "min-wick-ratio")

	_, err = NewWickRejection(0.5, -0.1, 0.5, 1, 2, types.PriceScale)
	assert.ErrorContains(t, err, "max-close-pos")

	_, err = NewWickRejection(0.5, 1.1, 0.5, 1, 2, types.PriceScale)
	assert.ErrorContains(t, err, "max-close-pos")

	_, err = NewWickRejection(0.5, 0.3, -1, 1, 2, types.PriceScale)
	assert.ErrorContains(t, err, "min-wick-atr")

	_, err = NewWickRejection(0.5, 0.3, 0.5, 0, 2, types.PriceScale)
	assert.ErrorContains(t, err, "lookback")

	_, err = NewWickRejection(0.5, 0.3, 0.5, 1, 0, types.PriceScale)
	assert.Error(t, err) // ATR period <= 0
}

func TestWickRejection_NotReadyBeforeATRWarmup(t *testing.T) {
	t.Parallel()
	w := newTestWickRejection(t)
	assert.False(t, w.Ready())

	w.Update([]market.Candle{quietCandle(100)})
	assert.False(t, w.Ready(), "ATR(2) needs 3 candles")

	w.Update([]market.Candle{quietCandle(100.2)})
	assert.False(t, w.Ready())

	w.Update([]market.Candle{quietCandle(100.1)})
	assert.True(t, w.Ready())
}

func TestWickRejection_MatchesBullishRejection(t *testing.T) {
	t.Parallel()
	w := newTestWickRejection(t)
	for _, c := range []float64{100, 100.2, 100.1} {
		w.Update([]market.Candle{quietCandle(c)})
	}
	require.True(t, w.Ready())
	assert.False(t, w.Matched(), "no rejection bar fed yet")

	w.Update([]market.Candle{bullishRejectionCandle()})
	assert.True(t, w.Matched())
	assert.Equal(t, types.Long, w.Side())
}

func TestWickRejection_MatchesBearishRejection(t *testing.T) {
	t.Parallel()
	w := newTestWickRejection(t)
	for _, c := range []float64{100, 100.2, 100.1} {
		w.Update([]market.Candle{quietCandle(c)})
	}
	require.True(t, w.Ready())

	w.Update([]market.Candle{bearishRejectionCandle()})
	assert.True(t, w.Matched())
	assert.Equal(t, types.Short, w.Side())
}

func TestWickRejection_NoMatchSmallWick(t *testing.T) {
	t.Parallel()
	w := newTestWickRejection(t)
	for _, c := range []float64{100, 100.2, 100.1} {
		w.Update([]market.Candle{quietCandle(c)})
	}
	require.True(t, w.Ready())

	// Symmetric small-range candle: no dominant wick either side.
	w.Update([]market.Candle{{Open: px(100), High: px(100.6), Low: px(99.4), Close: px(100.1)}})
	assert.False(t, w.Matched())
}

func TestWickRejection_NoMatchCloseTooFarFromOppositeExtreme(t *testing.T) {
	t.Parallel()
	w := newTestWickRejection(t)
	for _, c := range []float64{100, 100.2, 100.1} {
		w.Update([]market.Candle{quietCandle(c)})
	}
	require.True(t, w.Ready())

	// Long lower wick, but close sits mid-range, not near the high.
	w.Update([]market.Candle{{Open: px(100), High: px(101), Low: px(90), Close: px(95)}})
	assert.False(t, w.Matched())
}

func TestWickRejection_NoMatchWickBelowATRMultiple(t *testing.T) {
	t.Parallel()
	// min-wick-atr set very high: even a shape-qualifying wick must fail.
	w, err := NewWickRejection(0.5, 0.3, 50.0, 1, 2, types.PriceScale)
	require.NoError(t, err)
	for _, c := range []float64{100, 100.2, 100.1} {
		w.Update([]market.Candle{quietCandle(c)})
	}
	require.True(t, w.Ready())

	w.Update([]market.Candle{bullishRejectionCandle()})
	assert.False(t, w.Matched(), "wick length in ATR multiples must fail the very high threshold")
}

func TestWickRejection_LookbackAggregatesMultipleBars(t *testing.T) {
	t.Parallel()
	w, err := NewWickRejection(0.5, 0.3, 0.5, 2, 2, types.PriceScale)
	require.NoError(t, err)

	for _, c := range []float64{100, 100.2, 100.1} {
		w.Update([]market.Candle{quietCandle(c)})
	}
	assert.False(t, w.Ready(), "lookback=2 needs 2 candles in the window even once ATR is warm")

	// Two-bar window: bar 1 dips low (sets the rejection wick), bar 2
	// closes back up near the aggregate high. Neither bar alone need be a
	// pin bar; the aggregate of the two is.
	bar1 := market.Candle{Open: px(100), High: px(100.2), Low: px(90), Close: px(91)}
	bar2 := market.Candle{Open: px(91), High: px(100.5), Low: px(90.5), Close: px(100.3)}

	w.Update([]market.Candle{bar1})
	assert.False(t, w.Ready(), "lookback=2 needs 2 candles in the window, ATR being warm is not enough")

	w.Update([]market.Candle{bar1, bar2})
	assert.True(t, w.Ready())
	assert.True(t, w.Matched(), "aggregate of the two-bar window must read as a bullish rejection")
	assert.Equal(t, types.Long, w.Side())
}

func TestWickRejection_EmptyWindowClearsMatch(t *testing.T) {
	t.Parallel()
	w := newTestWickRejection(t)
	for _, c := range []float64{100, 100.2, 100.1} {
		w.Update([]market.Candle{quietCandle(c)})
	}
	w.Update([]market.Candle{bullishRejectionCandle()})
	require.True(t, w.Matched())

	w.Update(nil)
	assert.False(t, w.Matched())
}

func TestWickRejection_ZeroRangeCandleNoMatch(t *testing.T) {
	t.Parallel()
	w := newTestWickRejection(t)
	for _, c := range []float64{100, 100.2, 100.1} {
		w.Update([]market.Candle{quietCandle(c)})
	}
	require.True(t, w.Ready())

	w.Update([]market.Candle{{Open: px(100), High: px(100), Low: px(100), Close: px(100)}})
	assert.False(t, w.Matched())
}

func TestWickRejection_Name(t *testing.T) {
	t.Parallel()
	w := newTestWickRejection(t)
	assert.Equal(t, "wick-rejection", w.Name())
}
