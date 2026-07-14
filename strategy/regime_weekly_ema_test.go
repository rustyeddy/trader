package strategy

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// weeklyEMACT builds a CandleTime at a specific UTC timestamp with the given close price.
func weeklyEMACT(ts time.Time, closePrice types.Price) market.CandleTime {
	return market.CandleTime{
		Candle:    market.Candle{Open: closePrice, High: closePrice + 100, Low: closePrice - 100, Close: closePrice},
		Timestamp: types.FromTime(ts),
	}
}

// mondays returns n consecutive Monday timestamps starting from a base date.
func mondays(base time.Time, n int) []time.Time {
	out := make([]time.Time, n)
	// Find the next Monday from base (or base itself if it's Monday).
	for base.Weekday() != time.Monday {
		base = base.Add(24 * time.Hour)
	}
	for i := range out {
		out[i] = base.Add(time.Duration(i) * 7 * 24 * time.Hour)
	}
	return out
}

func TestWeeklyEMAFilter_NotReadyBeforeWarmup(t *testing.T) {
	t.Parallel()
	f, err := NewWeeklyEMAFilter(3, types.PriceScale)
	require.NoError(t, err)
	assert.False(t, f.Ready())
	// AllowSide returns true during warmup.
	assert.True(t, f.AllowSide(types.Long))
	assert.True(t, f.AllowSide(types.Short))
}

func TestWeeklyEMAFilter_TrendingAlwaysTrue(t *testing.T) {
	t.Parallel()
	f, err := NewWeeklyEMAFilter(3, types.PriceScale)
	require.NoError(t, err)
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	// Feed several weeks of data.
	for i, m := range mondays(base, 10) {
		f.Tick(weeklyEMACT(m, types.Price(100000+i*1000)))
	}
	assert.True(t, f.Trending(), "WeeklyEMAFilter.Trending must always return true")
}

func TestWeeklyEMAFilter_AllowsLongAboveEMA(t *testing.T) {
	t.Parallel()
	// EMA(3) on weekly closes: feed rising prices so last close is above EMA.
	f, err := NewWeeklyEMAFilter(3, types.PriceScale)
	require.NoError(t, err)
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	weeks := mondays(base, 10)

	// Rising prices: 100000, 101000, 102000, ...
	for i, m := range weeks {
		f.Tick(weeklyEMACT(m, types.Price(100000+i*1000)))
	}
	// Deliver one more bar in a new week with a high close to push above EMA.
	lastWeek := weeks[len(weeks)-1].Add(7 * 24 * time.Hour)
	f.Tick(weeklyEMACT(lastWeek, types.Price(120000)))

	require.True(t, f.Ready())
	assert.True(t, f.AllowSide(types.Long), "rising close above EMA must allow Long")
	assert.False(t, f.AllowSide(types.Short), "rising close above EMA must block Short")
}

func TestWeeklyEMAFilter_AllowsShortBelowEMA(t *testing.T) {
	t.Parallel()
	// EMA(3): feed falling prices so last close is below EMA.
	f, err := NewWeeklyEMAFilter(3, types.PriceScale)
	require.NoError(t, err)
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	weeks := mondays(base, 10)

	// Falling prices: 120000, 119000, 118000, ...
	for i, m := range weeks {
		f.Tick(weeklyEMACT(m, types.Price(120000-i*1000)))
	}
	// Deliver one more bar in a new week with a low close to push below EMA.
	lastWeek := weeks[len(weeks)-1].Add(7 * 24 * time.Hour)
	f.Tick(weeklyEMACT(lastWeek, types.Price(100000)))

	require.True(t, f.Ready())
	assert.False(t, f.AllowSide(types.Long), "falling close below EMA must block Long")
	assert.True(t, f.AllowSide(types.Short), "falling close below EMA must allow Short")
}

func TestWeeklyEMAFilter_AggregatesWithinWeek(t *testing.T) {
	t.Parallel()
	// Multiple H1 bars within the same ISO week should update only the
	// running weekly high/low/close, not advance the EMA.
	f, err := NewWeeklyEMAFilter(3, types.PriceScale)
	require.NoError(t, err)
	monday := time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC) // a Monday
	tuesday := monday.Add(24 * time.Hour)
	friday := monday.Add(4 * 24 * time.Hour)

	f.Tick(weeklyEMACT(monday, 100000))
	f.Tick(weeklyEMACT(tuesday, 101000))
	f.Tick(weeklyEMACT(friday, 102000))

	// EMA(3) needs 3 completed weekly bars; only intraweek bars have been fed.
	assert.False(t, f.Ready(), "EMA not ready before any week has closed")
}

func TestWeeklyEMAFilter_AllowSideUsesInProgressWeekClose(t *testing.T) {
	t.Parallel()

	f, err := NewWeeklyEMAFilter(3, types.PriceScale)
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	weeks := mondays(base, 10)

	for i, m := range weeks {
		f.Tick(weeklyEMACT(m, types.Price(100000+i*1000)))
	}

	currentWeek := weeks[len(weeks)-1].Add(7 * 24 * time.Hour)
	f.Tick(weeklyEMACT(currentWeek, types.Price(120000)))
	require.True(t, f.Ready())
	assert.True(t, f.AllowSide(types.Long), "high in-progress weekly close should allow Long")

	f.Tick(weeklyEMACT(currentWeek.Add(24*time.Hour), types.Price(90000)))
	assert.False(t, f.AllowSide(types.Long), "lower in-progress weekly close should flip Long permission")
	assert.True(t, f.AllowSide(types.Short), "lower in-progress weekly close should allow Short")
}

func TestWeeklyEMAFilter_FactoryRoundtrip(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{
		Kind:   "weekly-ema",
		Params: map[string]any{"period": 5},
	}, types.PriceScale)
	require.NoError(t, err)
	wf, ok := f.(*WeeklyEMAFilter)
	require.True(t, ok)
	assert.Equal(t, 5, wf.period)
}

func TestWeeklyEMAFilter_FactoryDefaults(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{Kind: "weekly-ema"}, types.PriceScale)
	require.NoError(t, err)
	wf, ok := f.(*WeeklyEMAFilter)
	require.True(t, ok)
	assert.Equal(t, 20, wf.period)
}

func TestWeeklyEMAFilter_Name(t *testing.T) {
	t.Parallel()
	f, err := NewWeeklyEMAFilter(20, types.PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "WeeklyEMA(20)", f.Name())
}

func TestWeeklyEMAFilter_EMAAccessorMatchesEMAValue(t *testing.T) {
	t.Parallel()

	f, err := NewWeeklyEMAFilter(3, types.PriceScale)
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, m := range mondays(base, 10) {
		f.Tick(weeklyEMACT(m, types.Price(100000+i*1000)))
	}
	f.Tick(weeklyEMACT(base.Add(10*7*24*time.Hour), types.Price(120000)))

	require.True(t, f.Ready())
	assert.Equal(t, f.EMA(), f.EMAValue())
}
