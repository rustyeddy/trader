package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// h1CT builds a CandleTime for a given UTC time and OHLC (as raw Price ints).
func h1CT(ts time.Time, o, h, l, c Price) CandleTime {
	return CandleTime{
		Candle:    Candle{Open: o, High: h, Low: l, Close: c},
		Timestamp: FromTime(ts),
	}
}

// feedDay sends 24 identical hourly bars for the given date.
func feedDay(f *D1ChoppinessFilter, date time.Time, o, h, l, c Price) {
	for hour := 0; hour < 24; hour++ {
		f.Tick(h1CT(date.Add(time.Duration(hour)*time.Hour), o, h, l, c))
	}
}

func TestD1ChoppinessFilter_NotReadyBeforeEnoughDays(t *testing.T) {
	t.Parallel()
	scale := Scale6(100_000)
	f := NewD1ChoppinessFilter(14, 61.8, scale)
	assert.False(t, f.Ready())
	assert.True(t, f.Trending(), "should allow trading while warming up")
}

func TestD1ChoppinessFilter_ReadyAfterPeriodDays(t *testing.T) {
	t.Parallel()
	scale := Scale6(100_000)
	f := NewD1ChoppinessFilter(5, 61.8, scale)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for d := 0; d < 5; d++ {
		feedDay(f, base.AddDate(0, 0, d), 10000, 11000, 9000, 10000)
	}
	// Feed one more day to trigger finalisation of day 5.
	feedDay(f, base.AddDate(0, 0, 5), 10000, 11000, 9000, 10000)
	assert.True(t, f.Ready())
}

func TestD1ChoppinessFilter_DayRolloverAggregation(t *testing.T) {
	t.Parallel()
	scale := Scale6(100_000)
	f := NewD1ChoppinessFilter(3, 61.8, scale)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Day 1: H1 bars with varying highs/lows — daily bar should be H=12000, L=8000.
	f.Tick(h1CT(base, 10000, 12000, 10000, 11000))
	f.Tick(h1CT(base.Add(1*time.Hour), 11000, 11500, 9000, 9500))
	f.Tick(h1CT(base.Add(2*time.Hour), 9500, 10000, 8000, 9000))

	// Day 2: triggers finalisation of day 1.
	day2 := base.AddDate(0, 0, 1)
	f.Tick(h1CT(day2, 9000, 9500, 8500, 9200))

	// The partial daily bar accumulator should now reflect day 2.
	assert.Equal(t, int64(day2.Unix())/86400, f.dayNum)
	assert.Equal(t, Price(9000), f.dayOpen)
	assert.Equal(t, Price(9500), f.dayHigh)
	assert.Equal(t, Price(8500), f.dayLow)
	assert.Equal(t, Price(9200), f.dayClose)
}

func TestD1ChoppinessFilter_SameDayBarsExtendAccumulator(t *testing.T) {
	t.Parallel()
	scale := Scale6(100_000)
	f := NewD1ChoppinessFilter(5, 61.8, scale)

	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	f.Tick(h1CT(base, 10000, 10500, 9800, 10200))
	f.Tick(h1CT(base.Add(1*time.Hour), 10200, 11000, 10100, 10800))
	f.Tick(h1CT(base.Add(2*time.Hour), 10800, 10900, 9500, 9600))

	assert.Equal(t, Price(10000), f.dayOpen,  "open from first bar")
	assert.Equal(t, Price(11000), f.dayHigh,  "high from second bar")
	assert.Equal(t, Price(9500),  f.dayLow,   "low from third bar")
	assert.Equal(t, Price(9600),  f.dayClose, "close from latest bar")
}

func TestD1ChoppinessFilter_TrendingBlocksWhenChoppy(t *testing.T) {
	t.Parallel()
	scale := Scale6(100_000)
	// Use period=3 so we can warm up quickly.
	f := NewD1ChoppinessFilter(3, 61.8, scale)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Feed 4 days of tight choppy bars (small range, high TR relative to HL).
	// A very choppy market: price oscillates between 10000 and 10010 each day.
	for d := 0; d < 4; d++ {
		feedDay(f, base.AddDate(0, 0, d), 10000, 10010, 9990, 10000)
	}
	// Feed a 5th day to finalise day 4.
	feedDay(f, base.AddDate(0, 0, 4), 10000, 10010, 9990, 10000)

	require.True(t, f.Ready())
	// Choppy market: CI should be high (> 61.8), so Trending() = false.
	assert.False(t, f.Trending(), "very choppy market should suppress entries (CI=%.1f)", f.Value())
}

func TestD1ChoppinessFilter_TrendingAllowsWhenTrending(t *testing.T) {
	t.Parallel()
	scale := Scale6(100_000)
	f := NewD1ChoppinessFilter(3, 61.8, scale)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Feed 4 days of strongly trending bars: each day price advances 500 pips
	// with a wide range, producing a low CI.
	for d := 0; d < 4; d++ {
		open := Price(10000 + d*500)
		high := open + 490
		low := open - 10
		close := high
		feedDay(f, base.AddDate(0, 0, d), open, high, low, close)
	}
	feedDay(f, base.AddDate(0, 0, 4), 12000, 12490, 11990, 12490)

	require.True(t, f.Ready())
	// Trending market: CI should be low (< 61.8), so Trending() = true.
	assert.True(t, f.Trending(), "strongly trending market should allow entries (CI=%.1f)", f.Value())
}

func TestD1ChoppinessFilter_WarmupAlwaysTrending(t *testing.T) {
	t.Parallel()
	scale := Scale6(100_000)
	f := NewD1ChoppinessFilter(14, 61.8, scale)

	// Even with choppy data, Trending() must return true until Ready.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for d := 0; d < 3; d++ {
		feedDay(f, base.AddDate(0, 0, d), 10000, 10005, 9995, 10000)
	}
	assert.False(t, f.Ready())
	assert.True(t, f.Trending(), "must not suppress entries during warmup")
}
