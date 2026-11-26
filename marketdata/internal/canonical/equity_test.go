package canonical

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func minute(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

// The dates ADR-095 section 4 requires, with UTC times.
func TestUSEquitySession_ADRFixtures(t *testing.T) {
	for _, c := range []struct {
		name        string
		year        int
		month       time.Month
		day         int
		open, close time.Time
		early       bool
	}{
		{"last day before DST", 2024, 3, 8, minute(2024, 3, 8, 14, 30), minute(2024, 3, 8, 21, 0), false},
		{"first day of DST", 2024, 3, 11, minute(2024, 3, 11, 13, 30), minute(2024, 3, 11, 20, 0), false},
		{"last day of DST", 2024, 11, 1, minute(2024, 11, 1, 13, 30), minute(2024, 11, 1, 20, 0), false},
		{"first day after DST", 2024, 11, 4, minute(2024, 11, 4, 14, 30), minute(2024, 11, 4, 21, 0), false},
		{"normal day", 2024, 11, 27, minute(2024, 11, 27, 14, 30), minute(2024, 11, 27, 21, 0), false},
		{"early close", 2024, 11, 29, minute(2024, 11, 29, 14, 30), minute(2024, 11, 29, 18, 0), true},
		{"early close in DST", 2024, 7, 3, minute(2024, 7, 3, 13, 30), minute(2024, 7, 3, 17, 0), true},
		{"first day after the storm closure", 2012, 10, 31, minute(2012, 10, 31, 13, 30), minute(2012, 10, 31, 20, 0), false},
		{"the day Stooq SPY lacks is a session", 2011, 2, 17, minute(2011, 2, 17, 14, 30), minute(2011, 2, 17, 21, 0), false},
		{"New Year on a Saturday closes no Friday", 2010, 12, 31, minute(2010, 12, 31, 14, 30), minute(2010, 12, 31, 21, 0), false},
		{"first covered session", 2005, 1, 3, minute(2005, 1, 3, 14, 30), minute(2005, 1, 3, 21, 0), false},
		{"last covered session", 2026, 12, 31, minute(2026, 12, 31, 14, 30), minute(2026, 12, 31, 21, 0), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := USEquitySession(c.year, c.month, c.day)
			require.NoError(t, err)
			assert.Equal(t, Session{Open: c.open, Close: c.close, Early: c.early}, s)

			at, err := USEquitySessionAt(c.open)
			require.NoError(t, err)
			assert.Equal(t, s, at)
		})
	}
}

func TestUSEquitySession_RefusesClosedDays(t *testing.T) {
	for name, d := range map[string]time.Time{
		"Thanksgiving":              utc(2024, 11, 28, 0),
		"Good Friday":               utc(2024, 3, 29, 0),
		"unscheduled storm":         utc(2012, 10, 29, 0),
		"storm, second day":         utc(2012, 10, 30, 0),
		"day of mourning 2007":      utc(2007, 1, 2, 0),
		"day of mourning 2018":      utc(2018, 12, 5, 0),
		"day of mourning 2025":      utc(2025, 1, 9, 0),
		"first observed Juneteenth": utc(2022, 6, 20, 0),
		"Saturday":                  utc(2024, 3, 2, 0),
		"Sunday":                    utc(2024, 3, 3, 0),
	} {
		_, err := USEquitySession(d.Year(), d.Month(), d.Day())
		assert.ErrorIs(t, err, ErrNonSession, name)
	}
}

func TestUSEquitySession_RefusesDatesOutsideTheCalendar(t *testing.T) {
	for name, d := range map[string]struct {
		y int
		m time.Month
		d int
	}{
		"before": {2004, 12, 31}, "after": {2027, 1, 4}, "no such date": {2023, 2, 29}, "month 13": {2024, 13, 1},
	} {
		_, err := USEquitySession(d.y, d.m, d.d)
		assert.ErrorIs(t, err, ErrOutsideCalendar, name)
	}
	_, err := USEquitySessionAt(minute(2027, 1, 4, 14, 30))
	assert.ErrorIs(t, err, ErrOutsideCalendar)
}

func TestUSEquitySessionAt_RefusesAnythingButTheOpen(t *testing.T) {
	for name, at := range map[string]time.Time{
		"the FX daily open":          minute(2024, 3, 1, 22, 0),
		"midnight UTC":               minute(2024, 3, 4, 0, 0), // Sunday evening in New York
		"winter offset in summer":    minute(2024, 3, 11, 14, 30),
		"a nanosecond after":         minute(2024, 3, 1, 14, 30).Add(time.Nanosecond),
		"the close, not the open":    minute(2024, 3, 1, 21, 0),
		"the zero time is too early": {},
	} {
		_, err := USEquitySessionAt(at)
		assert.Error(t, err, name)
	}
	_, err := USEquitySessionAt(minute(2024, 3, 1, 22, 0))
	assert.ErrorIs(t, err, ErrMisaligned)
}

func TestUSEquityBar_IsNotConsumableBeforeTheClose(t *testing.T) {
	s, err := USEquitySession(2024, 11, 29)
	require.NoError(t, err)
	b := tradeBar(t, 2024, 11, 29)

	assert.False(t, b.IsAvailable(s.Close.Add(-time.Nanosecond)))
	assert.True(t, b.IsAvailable(s.Close))
}

// The table's version is its name: data built with it must not see it change.
func TestEquityCalendar_IsPinned(t *testing.T) {
	sum := sha256.Sum256(usEquityCalendarCSV)
	assert.Equal(t, "f02de874b78cf3b50be1e0d03ac484eaa060a9dc064759922932afdca948f122", hex.EncodeToString(sum[:]),
		"%s is never edited once data is built with it: add a new version", CalendarUSEquityV1)
}

// Every full holiday year has nine or ten closures plus the listed unscheduled
// ones, so a dropped or doubled row shows.
func TestEquityCalendar_YearlyCounts(t *testing.T) {
	days, err := usEquityCalendar()
	require.NoError(t, err)
	closed := map[string]int{}
	early := 0
	for date, kind := range days {
		if kind == dayClosed {
			closed[date[:4]]++
		} else {
			early++
		}
	}
	assert.Equal(t, map[string]int{
		"2005": 8, "2006": 9, "2007": 10, "2008": 9, "2009": 9, "2010": 9, "2011": 8, "2012": 11,
		"2013": 9, "2014": 9, "2015": 9, "2016": 9, "2017": 9, "2018": 10, "2019": 9, "2020": 9,
		"2021": 9, "2022": 9, "2023": 10, "2024": 10, "2025": 11, "2026": 10,
	}, closed)
	assert.Equal(t, 47, early)
}

func TestParseEquityCalendar_RejectsMalformedTables(t *testing.T) {
	const head = "# source s https://example.test\ndate,kind,source\n"
	_, err := parseEquityCalendar([]byte(head + "2024-01-01,closed,s\n"))
	require.NoError(t, err)

	for name, text := range map[string]string{
		"no header":         "# source s https://example.test\n",
		"wrong header":      "date,kind\n",
		"source twice":      "# source s a\n# source s b\ndate,kind,source\n",
		"source, no url":    "# source s\ndate,kind,source\n",
		"field count":       head + "2024-01-01,closed\n",
		"bad date":          head + "2024-1-1,closed,s\n",
		"before the range":  head + "2004-01-01,closed,s\n",
		"after the range":   head + "2027-01-01,closed,s\n",
		"weekend":           head + "2024-01-06,closed,s\n",
		"out of order":      head + "2024-01-02,closed,s\n2024-01-01,closed,s\n",
		"duplicate":         head + "2024-01-01,closed,s\n2024-01-01,early,s\n",
		"unknown kind":      head + "2024-01-01,late,s\n",
		"undeclared source": head + "2024-01-01,closed,t\n",
		"line too long":     head + string(make([]byte, 70000)),
	} {
		_, err := parseEquityCalendar([]byte(text))
		assert.Error(t, err, name)
	}
}

func tradeBars(t *testing.T, days ...int) []marketdata.Bar {
	t.Helper()
	var bars []marketdata.Bar
	for _, d := range days {
		bars = append(bars, tradeBar(t, 2011, 2, d))
	}
	return bars
}

func TestUSEquityGaps_ClosuresAreNotGaps(t *testing.T) {
	// 19 and 20 are a weekend and 21 is Washington's Birthday.
	gaps, err := USEquityGaps(tradeBars(t, 16, 17, 18, 22))

	require.NoError(t, err)
	assert.Empty(t, gaps)
}

func TestUSEquityGaps_AMissingSessionIsAGap(t *testing.T) {
	gaps, err := USEquityGaps(tradeBars(t, 16, 18))

	require.NoError(t, err)
	assert.Equal(t, []Gap{{Start: minute(2011, 2, 17, 14, 30), End: minute(2011, 2, 18, 14, 30), Bars: 1, Closure: Open}}, gaps)
}

func TestUSEquityGaps_ARunCrossesClosuresAsOneGap(t *testing.T) {
	gaps, err := USEquityGaps(tradeBars(t, 16, 23))

	require.NoError(t, err)
	assert.Equal(t, []Gap{{Start: minute(2011, 2, 17, 14, 30), End: minute(2011, 2, 23, 14, 30), Bars: 3, Closure: Open}}, gaps)
}

func TestUSEquityGaps_RefusesABarThatIsNotASessionOpen(t *testing.T) {
	bars := tradeBars(t, 16, 17)
	bars[1].Time = bars[1].Time.Add(time.Hour)
	_, err := USEquityGaps(bars)
	assert.ErrorIs(t, err, ErrMisaligned)

	gaps, err := USEquityGaps(nil)
	require.NoError(t, err)
	assert.Empty(t, gaps)
}
