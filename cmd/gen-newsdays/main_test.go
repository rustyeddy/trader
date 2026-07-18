package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── firstFriday ───────────────────────────────────────────────────────────────

func TestFirstFriday_AlwaysReturnsFriday(t *testing.T) {
	for y := 2020; y <= 2026; y++ {
		for m := 1; m <= 12; m++ {
			got := firstFriday(y, m)
			assert.Equal(t, time.Friday, got.Weekday(),
				"firstFriday(%d, %d) = %s, want Friday", y, m, got.Format("2006-01-02"))
		}
	}
}

func TestFirstFriday_AlwaysInRequestedMonth(t *testing.T) {
	for y := 2020; y <= 2026; y++ {
		for m := 1; m <= 12; m++ {
			got := firstFriday(y, m)
			assert.Equal(t, time.Month(m), got.Month(),
				"firstFriday(%d, %d) returned wrong month", y, m)
			assert.Equal(t, y, got.Year(),
				"firstFriday(%d, %d) returned wrong year", y, m)
		}
	}
}

func TestFirstFriday_DayIsAtMostSeven(t *testing.T) {
	// The first Friday of any month is always between day 1 and day 7.
	for y := 2015; y <= 2026; y++ {
		for m := 1; m <= 12; m++ {
			got := firstFriday(y, m)
			assert.LessOrEqual(t, got.Day(), 7,
				"firstFriday(%d, %d).Day() = %d, want <= 7", y, m, got.Day())
			assert.GreaterOrEqual(t, got.Day(), 1)
		}
	}
}

func TestFirstFriday_KnownValues(t *testing.T) {
	cases := []struct {
		year, month int
		want        string
	}{
		{2024, 1, "2024-01-05"}, // Jan 1 is Monday → first Friday is Jan 5
		{2024, 2, "2024-02-02"}, // Feb 1 is Thursday → first Friday is Feb 2
		{2024, 3, "2024-03-01"}, // Mar 1 is Friday → first Friday is Mar 1
		{2024, 7, "2024-07-05"}, // Jul 1 is Monday → first Friday is Jul 5
		{2025, 1, "2025-01-03"}, // Jan 1 is Wednesday → first Friday is Jan 3
		{2020, 2, "2020-02-07"}, // Feb 1 is Saturday → first Friday is Feb 7
	}
	for _, tc := range cases {
		got := firstFriday(tc.year, tc.month)
		assert.Equal(t, tc.want, got.UTC().Format("2006-01-02"),
			"firstFriday(%d, %d)", tc.year, tc.month)
	}
}

// ── fomcDates table ───────────────────────────────────────────────────────────

func TestFOMCDates_AllParseSuccessfully(t *testing.T) {
	for _, d := range fomcDates {
		_, err := time.Parse("2006-01-02", d)
		require.NoError(t, err, "fomcDates entry %q should parse as YYYY-MM-DD", d)
	}
}

func TestFOMCDates_AlmostAllAreWeekdays(t *testing.T) {
	// Emergency meetings can fall on weekends; 2020-03-15 (COVID cut) is a known Sunday.
	weekendEmergencyDates := map[string]bool{
		"2020-03-15": true, // COVID emergency Sunday cut
	}
	for _, d := range fomcDates {
		if weekendEmergencyDates[d] {
			continue
		}
		ts, _ := time.Parse("2006-01-02", d)
		wd := ts.Weekday()
		assert.NotEqual(t, time.Saturday, wd, "FOMC date %s is a Saturday (unexpected)", d)
		assert.NotEqual(t, time.Sunday, wd, "FOMC date %s is a Sunday (unexpected)", d)
	}
}

func TestFOMCDates_SpansExpectedYearRange(t *testing.T) {
	minYear, maxYear := 9999, 0
	for _, d := range fomcDates {
		ts, _ := time.Parse("2006-01-02", d)
		if ts.Year() < minYear {
			minYear = ts.Year()
		}
		if ts.Year() > maxYear {
			maxYear = ts.Year()
		}
	}
	assert.Equal(t, 2005, minYear, "earliest FOMC year should be 2005")
	assert.GreaterOrEqual(t, maxYear, 2025, "FOMC dates should extend through at least 2025")
}

func TestFOMCDates_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool, len(fomcDates))
	for _, d := range fomcDates {
		assert.False(t, seen[d], "duplicate FOMC date: %s", d)
		seen[d] = true
	}
}

func TestFOMCDates_EachYearHasAtLeastEightMeetings(t *testing.T) {
	counts := make(map[int]int)
	for _, d := range fomcDates {
		ts, _ := time.Parse("2006-01-02", d)
		counts[ts.Year()]++
	}
	for year := 2005; year <= 2025; year++ {
		assert.GreaterOrEqual(t, counts[year], 8,
			"year %d has fewer than 8 FOMC meetings (%d)", year, counts[year])
	}
}
