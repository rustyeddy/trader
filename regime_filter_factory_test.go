package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRegimeFilter_Noop(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{Kind: ""}, PriceScale)
	require.NoError(t, err)
	assert.IsType(t, NoopRegime{}, f)
}

func TestGetRegimeFilter_Session(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{
		Kind:   "session",
		Params: map[string]any{"session_start": 8, "session_end": 16},
	}, PriceScale)
	require.NoError(t, err)
	sf, ok := f.(*SessionFilter)
	require.True(t, ok)
	assert.Equal(t, 8, sf.start)
	assert.Equal(t, 16, sf.end)
}

func TestGetRegimeFilter_SessionDefaults(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{Kind: "session"}, PriceScale)
	require.NoError(t, err)
	sf, ok := f.(*SessionFilter)
	require.True(t, ok)
	assert.Equal(t, 7, sf.start)
	assert.Equal(t, 17, sf.end)
}

func TestGetRegimeFilter_ADXD1(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{
		Kind:   "adx-d1",
		Params: map[string]any{"period": 10, "threshold": 25.0},
	}, PriceScale)
	require.NoError(t, err)
	af, ok := f.(*D1ADXFilter)
	require.True(t, ok)
	assert.Equal(t, 10, af.period)
	assert.Equal(t, 25.0, af.threshold)
}

func TestGetRegimeFilter_ADXD1Defaults(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{Kind: "adx-d1"}, PriceScale)
	require.NoError(t, err)
	af, ok := f.(*D1ADXFilter)
	require.True(t, ok)
	assert.Equal(t, 14, af.period)
	assert.Equal(t, 20.0, af.threshold)
}

func TestGetRegimeFilter_Composite(t *testing.T) {
	t.Parallel()
	cfg := RegimeConfig{
		Kind: "composite",
		Filters: []RegimeConfig{
			{Kind: "session", Params: map[string]any{"session_start": 7, "session_end": 17}},
			{Kind: "adx-d1", Params: map[string]any{"period": 14, "threshold": 20.0}},
		},
	}
	f, err := GetRegimeFilter(cfg, PriceScale)
	require.NoError(t, err)
	cf, ok := f.(*CompositeRegimeFilter)
	require.True(t, ok)
	assert.Len(t, cf.filters, 2)
}

func TestGetRegimeFilter_CompositeEmpty(t *testing.T) {
	t.Parallel()
	f, err := GetRegimeFilter(RegimeConfig{Kind: "composite"}, PriceScale)
	require.NoError(t, err)
	assert.IsType(t, NoopRegime{}, f)
}

func TestGetRegimeFilter_UnknownKind(t *testing.T) {
	t.Parallel()
	_, err := GetRegimeFilter(RegimeConfig{Kind: "does-not-exist"}, PriceScale)
	require.Error(t, err)
}

func TestCompositeRegimeFilter_TrendingRequiresAll(t *testing.T) {
	t.Parallel()

	// Session: 07-17, D1 ADX (always trending during warmup)
	sessionCfg := RegimeConfig{Kind: "session", Params: map[string]any{"session_start": 7, "session_end": 17}}
	adxCfg := RegimeConfig{Kind: "adx-d1", Params: map[string]any{"period": 3, "threshold": 1.0}}

	sf, _ := GetRegimeFilter(sessionCfg, PriceScale)
	af, _ := GetRegimeFilter(adxCfg, PriceScale)
	comp := NewCompositeRegimeFilter([]RegimeFilter{sf, af})

	inside := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)  // 10:00 UTC — inside session
	outside := time.Date(2024, 1, 2, 20, 0, 0, 0, time.UTC) // 20:00 UTC — outside session

	// ADX is in warmup (always trending=true). Session is inside window.
	comp.Tick(CandleTime{Candle: Candle{Open: 10000, High: 10100, Low: 9900, Close: 10050}, Timestamp: FromTime(inside)})
	assert.True(t, comp.Trending(), "inside session + ADX warmup = trending")

	// Move outside the session window.
	comp.Tick(CandleTime{Candle: Candle{Open: 10050, High: 10150, Low: 9950, Close: 10100}, Timestamp: FromTime(outside)})
	assert.False(t, comp.Trending(), "outside session = not trending regardless of ADX")
}

func TestCompositeRegimeFilter_AllowSideRequiresAll(t *testing.T) {
	t.Parallel()
	// weekly-ema blocks Short when rising; all others allow both sides.
	// Use a minimal weekly-ema (period=3) as the directional sub-filter.
	wf, err := NewWeeklyEMAFilter(3, PriceScale)
		require.NoError(t, err)
	comp := NewCompositeRegimeFilter([]RegimeFilter{wf})

	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	weeks := mondays(base, 10)

	// Feed rising closes so close ends up above EMA.
	for i, m := range weeks {
		wf.Tick(weeklyEMACT(m, Price(100000+i*1000)))
	}
	lastWeek := weeks[len(weeks)-1].Add(7 * 24 * time.Hour)
	wf.Tick(weeklyEMACT(lastWeek, Price(120000)))

	require.True(t, comp.Ready())
	assert.True(t, comp.AllowSide(Long), "composite must pass Long when sub-filter allows it")
	assert.False(t, comp.AllowSide(Short), "composite must block Short when any sub-filter blocks it")
}

func TestCompositeRegimeFilter_ReadyRequiresAll(t *testing.T) {
	t.Parallel()

	// Session is ready after first tick; ADX-D1 needs 2*period days.
	sessionCfg := RegimeConfig{Kind: "session"}
	adxCfg := RegimeConfig{Kind: "adx-d1", Params: map[string]any{"period": 14, "threshold": 20.0}}

	sf, _ := GetRegimeFilter(sessionCfg, PriceScale)
	af, _ := GetRegimeFilter(adxCfg, PriceScale)
	comp := NewCompositeRegimeFilter([]RegimeFilter{sf, af})

	ts := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	comp.Tick(CandleTime{Candle: Candle{Open: 10000, High: 10100, Low: 9900, Close: 10050}, Timestamp: FromTime(ts)})

	// Session is ready but ADX-D1 is not → composite not ready.
	assert.False(t, comp.Ready(), "composite not ready until all sub-filters are ready")
}
