package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTimeframeParseNormalizeAndString verifies expected behavior for this component.
func TestTimeframeParseNormalizeAndString(t *testing.T) {
	t.Parallel()

	tf, err := ParseTimeframe("m1")
	require.NoError(t, err)
	assert.Equal(t, M1, tf)

	tf, err = ParseTimeframe("H1")
	require.NoError(t, err)
	assert.Equal(t, H1, tf)

	tf, err = ParseTimeframe("D1")
	require.NoError(t, err)
	assert.Equal(t, D1, tf)

	tf, err = ParseTimeframe("D")
	require.NoError(t, err)
	assert.Equal(t, D1, tf) // OANDA uses "D" not "D1"

	tf, err = ParseTimeframe("d")
	require.NoError(t, err)
	assert.Equal(t, D1, tf)

	_, err = ParseTimeframe("unknown")
	require.Error(t, err)

	assert.Equal(t, "m1", normalizeTF("60"))
	assert.Equal(t, "h1", normalizeTF("3600"))
	assert.Equal(t, "m1", normalizeTF(" m1 "))

	assert.Equal(t, "m1", M1.String())
	assert.Equal(t, "timeframe(999)", Timeframe(999).String())
}

func TestParseTimeframe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want Timeframe
		err  bool
	}{
		{"M1", M1, false},
		{"m1", M1, false},
		{"H1", H1, false},
		{"h1", H1, false},
		{"D1", D1, false},
		{"D", D1, false},
		{"d1", D1, false},
		{"60", M1, false},
		{"3600", H1, false},
		{"86400", D1, false},
		{"tick", Ticks, false},
		{"1", Ticks, false},
		{"ticks", Ticks, false},
		{"W1", TF0, true},
		{"", TF0, true},
	}

	for _, tc := range tests {
		got, err := ParseTimeframe(tc.in)
		if tc.err {
			assert.Error(t, err, "input=%q", tc.in)
			continue
		}
		require.NoError(t, err, "input=%q", tc.in)
		assert.Equal(t, tc.want, got, "input=%q", tc.in)
	}
}

// TestParseTimeRange_OANDADailyAlias ensures "D" (OANDA's daily granularity)
// is accepted by ParseTimeRange, fixing the replay 422 on daily timeframes.
func TestParseTimeRange_OANDADailyAlias(t *testing.T) {
	t.Parallel()
	tr, err := ParseTimeRange("2024-01-01", "2024-03-01", "D")
	require.NoError(t, err)
	assert.Equal(t, D1, tr.TF)

	tr2, err := ParseTimeRange("2024-01-01", "2024-03-01", "D1")
	require.NoError(t, err)
	assert.Equal(t, D1, tr2.TF)
}

// TestTimeRangeBasics verifies expected behavior for this component.
func TestTimeRangeBasics(t *testing.T) {
	t.Parallel()

	r := newTimeRange(100, 200, M1)
	assert.True(t, r.Valid())
	assert.True(t, r.Contains(100))
	assert.False(t, r.Contains(200))
	assert.True(t, r.Overlaps(newTimeRange(150, 300, M1)))
	assert.True(t, r.Covers(newTimeRange(120, 180, M1)))
}

// TestIsFXMarketClosedWrapperMatchesCanonical verifies expected behavior for this component.
func TestIsFXMarketClosedWrapperMatchesCanonical(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	candles := []time.Time{
		time.Date(2024, 6, 7, 16, 59, 0, 0, ny),
		time.Date(2024, 6, 7, 17, 0, 0, 0, ny),
		time.Date(2024, 6, 9, 16, 59, 0, 0, ny),
		time.Date(2024, 6, 9, 17, 0, 0, 0, ny),
		time.Date(2024, 12, 24, 13, 0, 0, 0, ny),
	}
	for _, ts := range candles {
		assert.Equal(t, isForexMarketClosed(ts), isFXMarketClosed(ts))
	}
}

// TestSundayBeforeHolidayIsClosed verifies that the Sunday evening session is
// marked closed when Monday is New Year's Day or Christmas — eliminating the
// false-positive missing-slot reports that previously appeared for those months.
func TestSundayBeforeHolidayIsClosed(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	// Jan 1 2024 was a Monday → Dec 31 2023 Sunday evening should be closed.
	newYearEve := []time.Time{
		time.Date(2023, 12, 31, 17, 0, 0, 0, ny), // 5pm Sunday NY
		time.Date(2023, 12, 31, 20, 0, 0, 0, ny), // 8pm Sunday NY
		time.Date(2023, 12, 31, 23, 0, 0, 0, ny), // 11pm Sunday NY
	}
	for _, ts := range newYearEve {
		assert.True(t, isForexMarketClosed(ts), "expected closed: %v", ts)
	}

	// Dec 25 2023 was a Monday → Dec 24 2023 Sunday evening should be closed.
	christmasEve := []time.Time{
		time.Date(2023, 12, 24, 18, 0, 0, 0, ny),
		time.Date(2023, 12, 24, 22, 0, 0, 0, ny),
	}
	for _, ts := range christmasEve {
		assert.True(t, isForexMarketClosed(ts), "expected closed: %v", ts)
	}

	// Jan 1 2023 was itself a Sunday — evening session should be closed too.
	newYearSunday := []time.Time{
		time.Date(2023, 1, 1, 17, 0, 0, 0, ny),
		time.Date(2023, 1, 1, 20, 0, 0, 0, ny),
	}
	for _, ts := range newYearSunday {
		assert.True(t, isForexMarketClosed(ts), "expected closed (Jan 1 Sunday): %v", ts)
	}

	// Boxing Day (Dec 26) on a weekday should be closed.
	boxingDayWeekday := time.Date(2022, 12, 26, 10, 0, 0, 0, ny) // Monday
	assert.True(t, isForexMarketClosed(boxingDayWeekday), "expected closed (Boxing Day): %v", boxingDayWeekday)

	// Sunday before Boxing Day (Dec 25 Sun → Dec 26 Mon) evening should be closed.
	// Dec 25 2022 was a Sunday.
	sundayBeforeBoxingDay := time.Date(2022, 12, 25, 18, 0, 0, 0, ny)
	assert.True(t, isForexMarketClosed(sundayBeforeBoxingDay), "expected closed (Sunday before Boxing Day): %v", sundayBeforeBoxingDay)

	// A normal Sunday evening (not before a holiday) should be open.
	normalSundayEvening := time.Date(2024, 6, 2, 18, 0, 0, 0, ny)
	assert.False(t, isForexMarketClosed(normalSundayEvening), "expected open: %v", normalSundayEvening)

	// Sunday *before* the open (4pm NY) should still be closed regardless.
	sundayAfternoon := time.Date(2024, 6, 2, 16, 0, 0, 0, ny)
	assert.True(t, isForexMarketClosed(sundayAfternoon), "expected closed: %v", sundayAfternoon)
}

// TestTimestampHelpers verifies expected behavior for this component.
func TestTimestampHelpers(t *testing.T) {
	t.Parallel()

	ts := Timestamp(125)
	assert.Equal(t, Timestamp(120), ts.FloorToMinute())
	assert.Equal(t, Timestamp(0), ts.FloorToHour())
	assert.Equal(t, timemilli(125000), ts.Milli())
	assert.Equal(t, timemilli(120000), ts.Milli().FloorToMinute())
	assert.Equal(t, timemilli(0), ts.Milli().FloorToHour())
	assert.Equal(t, Timestamp(125), ts.MS().Sec())
	assert.True(t, Timestamp(124).Before(Timestamp(125)))
	assert.True(t, Timestamp(126).After(Timestamp(125)))
	assert.Equal(t, Timestamp(135), Timestamp(125).Add(10*time.Second))
}

// TestFromStringAndTimeRangeLocation verifies expected behavior for this component.
func TestFromStringAndTimeRangeLocation(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timestamp(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()), FromString("2024-01-15"))
	assert.Equal(t, Timestamp(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()), FromString(" 2024-01-15 "))
	assert.Equal(t, Timestamp(0), FromString("not-a-date"))

	ts, err := ParseDateTimestamp("2024-01-15")
	require.NoError(t, err)
	assert.Equal(t, Timestamp(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()), ts)

	_, err = ParseDateTimestamp("bad")
	require.Error(t, err)

	loc := time.UTC
	rng, err := timeRangeLocation("2024-01-01", "2024-01-10", "H1", loc)
	require.NoError(t, err)
	assert.True(t, rng.Valid())
	assert.Equal(t, H1, rng.TF)

	_, err = timeRangeLocation("2024-01-10", "2024-01-01", "H1", loc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid date range")

	_, err = timeRangeLocation("bad", "2024-01-10", "H1", loc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad from date")
}

// TestTimeRangeMonthsAndMonthRange verifies expected behavior for this component.
func TestTimeRangeMonthsAndMonthRange(t *testing.T) {
	t.Parallel()

	r := TimeRange{Start: Timestamp(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()), End: Timestamp(time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC).Unix()), TF: D1}
	months := r.MonthsInRange()
	require.Equal(t, []yearMonth{{Year: 2024, Month: 1}, {Year: 2024, Month: 2}, {Year: 2024, Month: 3}}, months)

	assert.Nil(t, TimeRange{}.MonthsInRange())

	mr := monthRange(2024, 2)
	assert.Equal(t, Timestamp(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC).Unix()), mr.Start)
	assert.Equal(t, Timestamp(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC).Unix()), mr.End)
	assert.False(t, TimeRange{}.Valid())
}
