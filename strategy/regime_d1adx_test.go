package strategy

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// feedDayADX sends 24 identical hourly bars for the given date to a D1ADXFilter.
func feedDayADX(f *D1ADXFilter, date time.Time, o, h, l, c types.Price) {
	for hour := 0; hour < 24; hour++ {
		f.Tick(h1CT(date.Add(time.Duration(hour)*time.Hour), o, h, l, c))
	}
}

func TestD1ADXFilter_NotReadyBeforeEnoughDays(t *testing.T) {
	t.Parallel()
	f, err := NewD1ADXFilter(14, 20.0, types.PriceScale)
	require.NoError(t, err)
	assert.False(t, f.Ready())
	assert.True(t, f.Trending(), "warmup must allow trading")
}

func TestD1ADXFilter_ReadyAfterWarmupDays(t *testing.T) {
	t.Parallel()
	// ADX needs 2×period days to become ready.
	period := 3
	f, err := NewD1ADXFilter(period, 20.0, types.PriceScale)
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Feed 2*period days + 1 trigger day.
	for d := 0; d <= 2*period; d++ {
		open := types.Price(10000 + d*100)
		feedDayADX(f, base.AddDate(0, 0, d), open, open+200, open-50, open+150)
	}
	assert.True(t, f.Ready())
}

func TestD1ADXFilter_DayRolloverAggregation(t *testing.T) {
	t.Parallel()
	f, err := NewD1ADXFilter(3, 20.0, types.PriceScale)
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Day 1: bars with known H/L extremes.
	f.Tick(h1CT(base, 10000, 12000, 10000, 11000))
	f.Tick(h1CT(base.Add(1*time.Hour), 11000, 11500, 9000, 9500))
	f.Tick(h1CT(base.Add(2*time.Hour), 9500, 10000, 8000, 9000))

	// Day 2: triggers finalisation of day 1.
	day2 := base.AddDate(0, 0, 1)
	f.Tick(h1CT(day2, 9000, 9500, 8500, 9200))

	assert.Equal(t, int64(day2.Unix())/86400, f.dayNum)
	assert.Equal(t, types.Price(9000), f.dayOpen)
	assert.Equal(t, types.Price(9500), f.dayHigh)
	assert.Equal(t, types.Price(8500), f.dayLow)
	assert.Equal(t, types.Price(9200), f.dayClose)
}

func TestD1ADXFilter_SameDayBarsExtendAccumulator(t *testing.T) {
	t.Parallel()
	f, err := NewD1ADXFilter(5, 20.0, types.PriceScale)
	require.NoError(t, err)

	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	f.Tick(h1CT(base, 10000, 10500, 9800, 10200))
	f.Tick(h1CT(base.Add(1*time.Hour), 10200, 11000, 10100, 10800))
	f.Tick(h1CT(base.Add(2*time.Hour), 10800, 10900, 9500, 9600))

	assert.Equal(t, types.Price(10000), f.dayOpen, "open from first bar")
	assert.Equal(t, types.Price(11000), f.dayHigh, "high from second bar")
	assert.Equal(t, types.Price(9500), f.dayLow, "low from third bar")
	assert.Equal(t, types.Price(9600), f.dayClose, "close from latest bar")
}

func TestD1ADXFilter_TrendingAllowsWhenTrending(t *testing.T) {
	t.Parallel()
	period := 3
	f, err := NewD1ADXFilter(period, 20.0, types.PriceScale)
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Feed strongly trending bars: each day price advances significantly.
	for d := 0; d <= 2*period; d++ {
		open := types.Price(10000 + d*500)
		feedDayADX(f, base.AddDate(0, 0, d), open, open+490, open-10, open+490)
	}

	require.True(t, f.Ready())
	assert.True(t, f.Trending(), "strongly trending market should allow entries (ADX=%.1f)", f.ADXValue())
}

func TestD1ADXFilter_BlocksWhenFlat(t *testing.T) {
	t.Parallel()
	period := 3
	// Use a high threshold (80) — even a moderately trending market won't pass.
	f, err := NewD1ADXFilter(period, 80.0, types.PriceScale)
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for d := 0; d <= 2*period; d++ {
		// Oscillating: even days go up slightly, odd days go down slightly.
		open := types.Price(10000)
		if d%2 == 0 {
			feedDayADX(f, base.AddDate(0, 0, d), open, open+50, open-10, open+40)
		} else {
			feedDayADX(f, base.AddDate(0, 0, d), open+40, open+50, open-10, open)
		}
	}

	require.True(t, f.Ready())
	assert.False(t, f.Trending(), "flat/choppy market should block entries with high threshold (ADX=%.1f)", f.ADXValue())
}

func TestD1ADXFilter_WarmupAlwaysTrending(t *testing.T) {
	t.Parallel()
	f, err := NewD1ADXFilter(14, 20.0, types.PriceScale)
	require.NoError(t, err)

	// Only feed 5 days — well under 2*14 warmup.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for d := 0; d < 5; d++ {
		feedDayADX(f, base.AddDate(0, 0, d), 10000, 10050, 9950, 10000)
	}
	assert.False(t, f.Ready())
	assert.True(t, f.Trending(), "must not suppress entries during warmup")
}

func TestD1ADXFilter_Name(t *testing.T) {
	t.Parallel()
	f, err := NewD1ADXFilter(14, 20.0, types.PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "D1-ADX(14,20.0)", f.Name())
}

func TestD1ADXFilter_RejectsInvalidThreshold(t *testing.T) {
	t.Parallel()

	_, err := NewD1ADXFilter(14, -0.1, types.PriceScale)
	require.Error(t, err)

	_, err = NewD1ADXFilter(14, 100.1, types.PriceScale)
	require.Error(t, err)
}

func TestD1ADXFilter_ADXAccessorMatchesADXValue(t *testing.T) {
	t.Parallel()

	f, err := NewD1ADXFilter(3, 20.0, types.PriceScale)
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for d := 0; d <= 6; d++ {
		open := types.Price(10000 + d*500)
		feedDayADX(f, base.AddDate(0, 0, d), open, open+490, open-10, open+490)
	}

	require.True(t, f.Ready())
	assert.Equal(t, f.ADX(), f.ADXValue())
}
