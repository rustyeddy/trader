package canonical

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// at is a New York wall-clock time as a UTC instant.
func at(t *testing.T, y int, m time.Month, d, h int) time.Time {
	t.Helper()
	return time.Date(y, m, d, h, 0, 0, 0, ny(t)).UTC()
}

func TestClosureAt_H1(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		want Closure
	}{
		{"Friday 16:00 is the last hour before the close", at(t, 2024, time.March, 1, 16), Open},
		{"Friday 17:00 is closed", at(t, 2024, time.March, 1, 17), Weekend},
		{"Saturday", at(t, 2024, time.March, 2, 3), Weekend},
		{"Sunday 16:00 is closed", at(t, 2024, time.March, 3, 16), Weekend},
		{"Sunday 17:00 reopens", at(t, 2024, time.March, 3, 17), Open},
		{"a weekday", at(t, 2024, time.March, 5, 10), Open},
		// The week of the spring-forward change (March 10 2024): Friday 17:00 New
		// York is 21:00 UTC before it and 21:00 UTC is Friday 17:00 after it; the
		// rule follows New York time, not a fixed UTC hour.
		{"Friday 17:00 after the clocks change", at(t, 2024, time.March, 15, 17), Weekend},
		{"Sunday 17:00 after the clocks change", at(t, 2024, time.March, 17, 17), Open},
		{"Christmas Eve 11:00 is before the window", at(t, 2024, time.December, 24, 11), Open},
		{"Christmas Eve 12:00 opens the window", at(t, 2024, time.December, 24, 12), Holiday},
		{"Christmas Day", at(t, 2024, time.December, 25, 9), Holiday},
		{"Boxing Day 17:00 is in the window", at(t, 2024, time.December, 26, 17), Holiday},
		{"Boxing Day 18:00 is after it", at(t, 2024, time.December, 26, 18), Open},
		{"New Year's Eve 11:00", at(t, 2024, time.December, 31, 11), Open},
		{"New Year's Eve 12:00", at(t, 2024, time.December, 31, 12), Holiday},
		{"January 1", at(t, 2025, time.January, 1, 20), Holiday},
		{"January 2 17:00 is in the window", at(t, 2025, time.January, 2, 17), Holiday},
		{"January 2 18:00 is after it", at(t, 2025, time.January, 2, 18), Open},
		{"a weekend inside a holiday window is a weekend", at(t, 2022, time.December, 25, 3), Weekend},
		{"midsummer", at(t, 2024, time.July, 4, 12), Open},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClosureAt(H1, tc.when)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClosureAt_D1(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		want Closure
	}{
		{"Sunday 17:00 opens Monday's day", at(t, 2024, time.March, 3, 17), Open},
		{"Thursday 17:00 opens Friday's day", at(t, 2024, time.March, 7, 17), Open},
		{"Friday 17:00 has no bar", at(t, 2024, time.March, 8, 17), Weekend},
		{"Saturday 17:00 has no bar", at(t, 2024, time.March, 9, 17), Weekend},
		{"Christmas Eve 17:00 is the Christmas day", at(t, 2024, time.December, 24, 17), Holiday},
		{"New Year's Eve 17:00", at(t, 2024, time.December, 31, 17), Holiday},
		{"Boxing Day 17:00", at(t, 2024, time.December, 26, 17), Holiday},
		{"the day before Christmas Eve", at(t, 2024, time.December, 23, 17), Open},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClosureAt(D1, tc.when)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClosureAt_RefusesAnUnsupportedInterval(t *testing.T) {
	_, err := ClosureAt(Interval("h4"), utc(2024, time.March, 5, 10))
	assert.ErrorIs(t, err, ErrUnsupportedInterval)
}

// barsAt returns bars opening at each of the given times.
func barsAt(t *testing.T, times ...time.Time) []marketdata.Bar {
	t.Helper()
	var out []marketdata.Bar
	for _, ts := range times {
		out = append(out, goodBar(t, ts))
	}
	return out
}

// run lists n times from from, step apart.
func run(from time.Time, n int, step time.Duration) []time.Time {
	var out []time.Time
	for i := range n {
		out = append(out, from.Add(time.Duration(i)*step))
	}
	return out
}

func TestGaps_NoneForContiguousBars(t *testing.T) {
	got, err := Gaps(H1, barsAt(t, run(utc(2024, time.March, 5, 0), 30, time.Hour)...))

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGaps_NothingToJudgeForFewerThanTwoBars(t *testing.T) {
	for _, bars := range [][]marketdata.Bar{nil, barsAt(t, utc(2024, time.March, 5, 0))} {
		got, err := Gaps(H1, bars)
		require.NoError(t, err)
		assert.Empty(t, got)
	}
}

func TestGaps_AnUnexplainedRunIsOneGapWithItsBounds(t *testing.T) {
	got, err := Gaps(H1, barsAt(t, utc(2024, time.March, 5, 0), utc(2024, time.March, 5, 1), utc(2024, time.March, 5, 5)))

	require.NoError(t, err)
	assert.Equal(t, []Gap{{Start: utc(2024, time.March, 5, 2), End: utc(2024, time.March, 5, 5), Bars: 3, Closure: Open}}, got)
}

func TestGaps_ASingleMissingBarIsAGap(t *testing.T) {
	got, err := Gaps(H1, barsAt(t, utc(2024, time.March, 5, 0), utc(2024, time.March, 5, 2)))

	require.NoError(t, err)
	assert.Equal(t, []Gap{{Start: utc(2024, time.March, 5, 1), End: utc(2024, time.March, 5, 2), Bars: 1}}, got)
}

func TestGaps_TheWeeklyCloseIsNotReported(t *testing.T) {
	got, err := Gaps(H1, barsAt(t, at(t, 2024, time.March, 1, 16), at(t, 2024, time.March, 3, 17)))

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGaps_AWeekendThatStartsLateOrEndsEarlyLeavesTheRestAsGaps(t *testing.T) {
	// The market should have traded Friday 14:00 to 16:59 and from Sunday 17:00,
	// and did neither: the last bar is Friday 13:00 and the next Sunday 20:00.
	got, err := Gaps(H1, barsAt(t, at(t, 2024, time.March, 1, 13), at(t, 2024, time.March, 3, 20)))

	require.NoError(t, err)
	assert.Equal(t, []Gap{
		{Start: at(t, 2024, time.March, 1, 14), End: at(t, 2024, time.March, 1, 17), Bars: 3},
		{Start: at(t, 2024, time.March, 3, 17), End: at(t, 2024, time.March, 3, 20), Bars: 3},
	}, got)
}

func TestGaps_AHolidayIsReportedAsAClosure(t *testing.T) {
	// Christmas 2024 as the archive has it: last bar Tuesday 16:00 New York, next
	// Wednesday 17:00.
	got, err := Gaps(H1, barsAt(t, at(t, 2024, time.December, 24, 16), at(t, 2024, time.December, 25, 17)))

	require.NoError(t, err)
	assert.Equal(t, []Gap{{Start: at(t, 2024, time.December, 24, 17), End: at(t, 2024, time.December, 25, 17), Bars: 24, Closure: Holiday}}, got)
}

func TestGaps_AGapThatLeavesAHolidayWindowIsSplit(t *testing.T) {
	// Boxing Day 2023 (a Tuesday): the window ends at 17:59, so a reopening at
	// 20:00 leaves two hours the calendar does not explain.
	got, err := Gaps(H1, barsAt(t, at(t, 2023, time.December, 26, 15), at(t, 2023, time.December, 26, 20)))

	require.NoError(t, err)
	assert.Equal(t, []Gap{
		{Start: at(t, 2023, time.December, 26, 16), End: at(t, 2023, time.December, 26, 18), Bars: 2, Closure: Holiday},
		{Start: at(t, 2023, time.December, 26, 18), End: at(t, 2023, time.December, 26, 20), Bars: 2, Closure: Open},
	}, got)
}

func TestGaps_EveryGapOfABarSetIsFound(t *testing.T) {
	got, err := Gaps(H1, barsAt(t, utc(2024, time.March, 5, 0), utc(2024, time.March, 5, 2), utc(2024, time.March, 5, 3), utc(2024, time.March, 5, 7)))

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].Bars)
	assert.Equal(t, 3, got[1].Bars)
}

func TestGaps_D1(t *testing.T) {
	t.Run("a missing weekday", func(t *testing.T) {
		got, err := Gaps(D1, barsAt(t, at(t, 2024, time.March, 4, 17), at(t, 2024, time.March, 6, 17)))
		require.NoError(t, err)
		assert.Equal(t, []Gap{{Start: at(t, 2024, time.March, 5, 17), End: at(t, 2024, time.March, 6, 17), Bars: 1}}, got)
	})
	t.Run("Thursday to Sunday is the weekend", func(t *testing.T) {
		got, err := Gaps(D1, barsAt(t, at(t, 2024, time.March, 7, 17), at(t, 2024, time.March, 10, 17)))
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("across the clocks changing, a day is 23 hours", func(t *testing.T) {
		// Sunday March 10 2024 is the spring-forward day: Sunday 17:00 to Monday
		// 17:00 New York is 23 hours.
		got, err := Gaps(D1, barsAt(t, at(t, 2024, time.March, 10, 17), at(t, 2024, time.March, 11, 17), at(t, 2024, time.March, 12, 17)))
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("Christmas", func(t *testing.T) {
		got, err := Gaps(D1, barsAt(t, at(t, 2024, time.December, 23, 17), at(t, 2024, time.December, 25, 17)))
		require.NoError(t, err)
		assert.Equal(t, []Gap{{Start: at(t, 2024, time.December, 24, 17), End: at(t, 2024, time.December, 25, 17), Bars: 1, Closure: Holiday}}, got)
	})
}

func TestGaps_RefusesAnUnsupportedInterval(t *testing.T) {
	_, err := Gaps(Interval("h4"), barsAt(t, utc(2024, time.March, 5, 0), utc(2024, time.March, 5, 8)))
	assert.ErrorIs(t, err, ErrUnsupportedInterval)
}

// The gaps the OANDA archive actually has, found by listing every hour missing
// from the 2005 to 2026 EURUSD and USDJPY h1 files outside the weekly close. The
// calendar must call each year-end one a holiday and the rest unexplained: these
// are the cases that fixed the windows.
func TestGaps_TheArchivesOwnGaps(t *testing.T) {
	tests := []struct {
		name       string
		last, next time.Time // the stored bars either side
		want       Closure
	}{
		{"2015 early close", at(t, 2015, time.December, 24, 13), at(t, 2015, time.December, 25, 17), Holiday},
		{"2012 Christmas reopens Wednesday morning", at(t, 2012, time.December, 24, 13), at(t, 2012, time.December, 26, 8), Holiday},
		{"2012 New Year's Eve", at(t, 2012, time.December, 31, 13), at(t, 2013, time.January, 1, 20), Holiday},
		{"2016 Christmas Sunday, reopening Monday", at(t, 2016, time.December, 23, 16), at(t, 2016, time.December, 26, 18), Holiday},
		{"2017 New Year's Day Sunday, reopening Tuesday", at(t, 2016, time.December, 30, 16), at(t, 2017, time.January, 2, 18), Holiday},
		{"2008 New Year's Day morning", at(t, 2008, time.January, 1, 4), at(t, 2008, time.January, 1, 13), Holiday},
		{"2007 January 8", at(t, 2007, time.January, 8, 17), at(t, 2007, time.January, 9, 16), Open},
		{"2022 May 12 two hours", at(t, 2022, time.May, 12, 1), at(t, 2022, time.May, 12, 4), Open},
		{"2024 May 20 two hours", at(t, 2024, time.May, 20, 10), at(t, 2024, time.May, 20, 13), Open},
		{"2019 reopening an hour late", at(t, 2019, time.October, 18, 16), at(t, 2019, time.October, 20, 19), Open},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Gaps(H1, barsAt(t, tc.last, tc.next))
			require.NoError(t, err)
			var holiday, other int
			for _, g := range got {
				if g.Closure == Holiday {
					holiday += g.Bars
				} else {
					other += g.Bars
				}
			}
			switch tc.want {
			case Holiday:
				assert.NotZero(t, holiday)
				assert.Zero(t, other, "nothing outside the holiday window: %v", got)
			default:
				assert.NotZero(t, other, "the calendar does not explain it: %v", got)
			}
		})
	}
}
