package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nyTime(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, newYorkLocation)
}

func TestFXCalendarStatusFridayCloseBoundary(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 2, 16, 59)), "just before Friday close")
	assert.Equal(t, StatusClosed, c.Status(nyTime(2026, time.January, 2, 17, 0)), "exactly at Friday close")
	assert.Equal(t, StatusClosed, c.Status(nyTime(2026, time.January, 2, 17, 1)), "just after Friday close")
}

func TestFXCalendarStatusSundayOpenBoundary(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	assert.Equal(t, StatusClosed, c.Status(nyTime(2026, time.January, 4, 16, 59)), "just before Sunday open")
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 4, 17, 0)), "exactly at Sunday open")
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 4, 17, 1)), "just after Sunday open")
}

func TestFXCalendarStatusSaturdayAlwaysClosed(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	assert.Equal(t, StatusClosed, c.Status(nyTime(2026, time.January, 3, 0, 0)))
	assert.Equal(t, StatusClosed, c.Status(nyTime(2026, time.January, 3, 12, 0)))
	assert.Equal(t, StatusClosed, c.Status(nyTime(2026, time.January, 3, 23, 59)))
}

func TestFXCalendarStatusOrdinaryWeekdayOpen(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 6, 3, 0)), "Tuesday overnight")
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 7, 12, 0)), "Wednesday midday")
}

func TestFXCalendarSessionAlignsToRollover(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	session, ok := c.Session(nyTime(2026, time.January, 6, 10, 0)) // Tuesday
	require.True(t, ok)
	assert.Equal(t, nyTime(2026, time.January, 5, 17, 0), session.Start(), "Monday 17:00 NY open")
	assert.Equal(t, nyTime(2026, time.January, 6, 17, 0), session.End(), "Tuesday 17:00 NY close")
}

func TestFXCalendarSessionMondayMorningBelongsToSundayOpenSession(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	session, ok := c.Session(nyTime(2026, time.January, 5, 10, 0)) // Monday morning
	require.True(t, ok)
	assert.Equal(t, nyTime(2026, time.January, 4, 17, 0), session.Start(), "session opened Sunday 17:00 NY")
	assert.Equal(t, nyTime(2026, time.January, 5, 17, 0), session.End())
}

func TestFXCalendarSessionNotOkWhenClosed(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	_, ok := c.Session(nyTime(2026, time.January, 3, 12, 0)) // Saturday
	assert.False(t, ok)
}

func TestFXCalendarDayBarOrdinaryWeekday(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	bar, err := c.Bar(nyTime(2026, time.January, 7, 9, 0), D1) // Wednesday
	require.NoError(t, err)
	assert.Equal(t, nyTime(2026, time.January, 6, 17, 0), bar.Start())
	assert.Equal(t, nyTime(2026, time.January, 7, 17, 0), bar.End())
	assert.Equal(t, 24*time.Hour, bar.Duration())
}

func TestFXCalendarWeekBarAlignsToSundayOpen(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	bar, err := c.Bar(nyTime(2026, time.January, 7, 9, 0), W1) // Wednesday
	require.NoError(t, err)
	assert.Equal(t, nyTime(2026, time.January, 4, 17, 0), bar.Start(), "prior Sunday open")
	assert.Equal(t, nyTime(2026, time.January, 11, 17, 0), bar.End(), "following Sunday open")
}

func TestFXCalendarBarRejectsMultiDayAndMultiWeekCounts(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	d2, err := NewInterval(UnitDay, 2)
	require.NoError(t, err)
	_, err = c.Bar(nyTime(2026, time.January, 7, 9, 0), d2)
	assert.Error(t, err)

	w2, err := NewInterval(UnitWeek, 2)
	require.NoError(t, err)
	_, err = c.Bar(nyTime(2026, time.January, 7, 9, 0), w2)
	assert.Error(t, err)
}

func TestFXCalendarBarUTCAlignmentForHourAndMinute(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	t9h13 := time.Date(2026, time.January, 7, 9, 13, 27, 0, time.UTC)

	h4, err := c.Bar(t9h13, H4)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.January, 7, 8, 0, 0, 0, time.UTC), h4.Start())
	assert.Equal(t, time.Date(2026, time.January, 7, 12, 0, 0, 0, time.UTC), h4.End())

	m1, err := c.Bar(t9h13, M1)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.January, 7, 9, 13, 0, 0, time.UTC), m1.Start())
	assert.Equal(t, time.Date(2026, time.January, 7, 9, 14, 0, 0, time.UTC), m1.End())
}

func TestFXCalendarBarRejectsUnknownUnit(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	_, err := c.Bar(nyTime(2026, time.January, 7, 9, 0), Interval{unit: Unit(200), count: 1})
	assert.Error(t, err)
}

// The zero Interval carries UnitMinute with a zero count, which would
// otherwise pass the unit switch and surface as an empty-range error; Bar
// must reject it up front as an invalid interval.
func TestFXCalendarBarRejectsZeroInterval(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	_, err := c.Bar(nyTime(2026, time.January, 7, 9, 0), Interval{})
	assert.Error(t, err)
}

// 2026-03-08 is the US spring-forward transition: New York clocks jump
// from 02:00 to 03:00, so the trading day containing that instant loses
// an hour of wall-clock time.
func TestFXCalendarDayBarSpringForwardLosesAnHour(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	bar, err := c.Bar(nyTime(2026, time.March, 8, 10, 0), D1)
	require.NoError(t, err)
	assert.Equal(t, nyTime(2026, time.March, 7, 17, 0), bar.Start())
	assert.Equal(t, nyTime(2026, time.March, 8, 17, 0), bar.End())
	assert.Equal(t, 23*time.Hour, bar.Duration(), "spring-forward day is 23 hours")
}

// 2026-11-01 is the US fall-back transition: New York clocks repeat
// 01:00-02:00, so the trading day containing that instant gains an hour.
func TestFXCalendarDayBarFallBackGainsAnHour(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	bar, err := c.Bar(nyTime(2026, time.November, 1, 10, 0), D1)
	require.NoError(t, err)
	assert.Equal(t, nyTime(2026, time.October, 31, 17, 0), bar.Start())
	assert.Equal(t, nyTime(2026, time.November, 1, 17, 0), bar.End())
	assert.Equal(t, 25*time.Hour, bar.Duration(), "fall-back day is 25 hours")
}

// The week opening 2026-03-01 17:00 NY spans the spring-forward
// transition on 2026-03-08, so it is one hour short of 7*24h.
func TestFXCalendarWeekBarSpringForwardLosesAnHour(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	bar, err := c.Bar(nyTime(2026, time.March, 3, 9, 0), W1) // Tuesday within that week
	require.NoError(t, err)
	assert.Equal(t, nyTime(2026, time.March, 1, 17, 0), bar.Start())
	assert.Equal(t, nyTime(2026, time.March, 8, 17, 0), bar.End())
	assert.Equal(t, 7*24*time.Hour-time.Hour, bar.Duration())
}

// The week opening 2026-10-25 17:00 NY spans the fall-back transition on
// 2026-11-01, so it is one hour longer than 7*24h.
func TestFXCalendarWeekBarFallBackGainsAnHour(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})

	bar, err := c.Bar(nyTime(2026, time.October, 29, 9, 0), W1) // Thursday within that week
	require.NoError(t, err)
	assert.Equal(t, nyTime(2026, time.October, 25, 17, 0), bar.Start())
	assert.Equal(t, nyTime(2026, time.November, 1, 17, 0), bar.End())
	assert.Equal(t, 7*24*time.Hour+time.Hour, bar.Duration())
}

// The holiday closure covers the entire rollover-to-rollover session
// that ends on the named date, not just that calendar day's own hours —
// Status and Session must agree on every instant within that session.
func TestFXCalendarHolidayClosesTheWholeSessionThatEndsOnIt(t *testing.T) {
	holiday := nyTime(2026, time.January, 1, 0, 0) // New Year's Day, a Thursday
	c := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{holiday}})

	// The session ending Jan 1 17:00 NY opens Dec 31 17:00 NY.
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2025, time.December, 31, 20, 0)), "evening portion of the session ending on the holiday")
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.January, 1, 10, 0)))
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.January, 1, 16, 59)))

	_, ok := c.Session(nyTime(2025, time.December, 31, 20, 0))
	assert.False(t, ok, "holiday session has no Session")
	_, ok = c.Session(nyTime(2026, time.January, 1, 10, 0))
	assert.False(t, ok, "holiday has no Session")

	// The next session, ending Jan 2, is unaffected.
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 1, 20, 0)))
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 2, 10, 0)))
}

// Regression coverage for the previous Status/Session disagreement:
// every instant inside a returned Session must itself report StatusOpen,
// and Session's ok must exactly track Status == StatusOpen, across a
// window that includes a configured holiday.
func TestFXCalendarStatusAndSessionAgreeAcrossAHoliday(t *testing.T) {
	holiday := nyTime(2026, time.January, 1, 0, 0)
	c := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{holiday}})

	start := nyTime(2025, time.December, 29, 0, 0)
	for i := range 14 * 24 {
		at := start.Add(time.Duration(i) * time.Hour)

		session, ok := c.Session(at)
		open := c.Status(at) == StatusOpen
		require.Equal(t, open, ok, "Session ok must track Status==StatusOpen at %s", at)
		if !ok {
			continue
		}
		require.True(t, session.Contains(at))
		assert.Equal(t, StatusOpen, c.Status(session.Start()), "session start must be open")
		assert.Equal(t, StatusOpen, c.Status(session.End().Add(-time.Nanosecond)), "instant just before session end must be open")
	}
}

func TestFXCalendarHolidayUsesLiteralDateFieldsNotConvertedLocation(t *testing.T) {
	// 03:00 UTC on Jan 2 is Jan 1 22:00 EST in New York — a different
	// civil date. The holiday's literal Year/Month/Day (Jan 2) is what
	// must match, regardless of Location.
	holiday := time.Date(2026, time.January, 2, 3, 0, 0, 0, time.UTC)
	c := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{holiday}})

	// Session ending Jan 2 17:00 NY (opens Jan 1 17:00 NY) is the holiday.
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.January, 2, 10, 0)))
	// Session ending Jan 1 17:00 NY is not.
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 1, 10, 0)))
}

func TestFXCalendarImplementsCalendar(t *testing.T) {
	var _ Calendar = NewFXCalendar(FXCalendarParams{})
}

// --- Partial closures (issue #74) ---

func TestFXCalendarPartialClosureBeforeThresholdStillOpen(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{
		PartialClosures: []PartialClosure{
			{Date: nyTime(2026, time.December, 24, 0, 0), From: 13 * time.Hour},
		},
	})
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.December, 24, 12, 59)))
}

func TestFXCalendarPartialClosureAtThresholdClosed(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{
		PartialClosures: []PartialClosure{
			{Date: nyTime(2026, time.December, 24, 0, 0), From: 13 * time.Hour},
		},
	})
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.December, 24, 13, 0)), "exactly at the threshold")
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.December, 24, 16, 59)), "just before the normal session end")
}

func TestFXCalendarPartialClosureEndsAtNormalRollover(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{
		PartialClosures: []PartialClosure{
			{Date: nyTime(2026, time.December, 24, 0, 0), From: 13 * time.Hour},
		},
	})
	// The following session (ending Dec 25) is unaffected by Dec 24's
	// partial closure — this fixture has no Dec 25 holiday configured.
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.December, 24, 17, 0)))
}

func TestFXCalendarPartialClosureSessionHasNoSessionWhileClosed(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{
		PartialClosures: []PartialClosure{
			{Date: nyTime(2026, time.December, 24, 0, 0), From: 13 * time.Hour},
		},
	})
	_, ok := c.Session(nyTime(2026, time.December, 24, 14, 0))
	assert.False(t, ok)

	session, ok := c.Session(nyTime(2026, time.December, 24, 9, 0))
	require.True(t, ok, "still open before the threshold")
	assert.True(t, session.Contains(nyTime(2026, time.December, 24, 9, 0)))
}

func TestFXCalendarPartialClosureUsesLiteralDateFields(t *testing.T) {
	// Same literal-date convention as Holidays: a UTC time.Time whose
	// literal Y/M/D is Dec 24 configures the closure for the session
	// ending on Dec 24 New York, regardless of the value's own Location.
	pc := PartialClosure{Date: time.Date(2026, time.December, 24, 3, 0, 0, 0, time.UTC), From: 13 * time.Hour}
	c := NewFXCalendar(FXCalendarParams{PartialClosures: []PartialClosure{pc}})
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.December, 24, 14, 0)))
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.December, 24, 12, 0)))
}

// --- Sunday-itself-holiday special case (issue #74) ---

// 2023-01-01 is a Sunday. Legacy (trader-first-try) keeps the Sunday
// evening session closed in this case even though the following Monday,
// 2023-01-02, is not itself a holiday date.
func TestFXCalendarSundayItselfHolidayKeepsWeekClosed(t *testing.T) {
	holiday := nyTime(2023, time.January, 1, 0, 0)
	c := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{holiday}})

	assert.Equal(t, StatusClosed, c.Status(nyTime(2023, time.January, 1, 16, 0)), "still the ordinary Sunday-before-open closure")
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2023, time.January, 1, 17, 0)), "would normally reopen the week")
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2023, time.January, 1, 23, 0)))

	// The following Monday session (ending Jan 2, not itself a holiday
	// date) opens normally once we're past the Sunday-evening exception.
	assert.Equal(t, StatusOpen, c.Status(nyTime(2023, time.January, 2, 10, 0)))
}

func TestFXCalendarOrdinarySundayEveningOpensNormally(t *testing.T) {
	// A Sunday that is not itself a holiday opens as usual, even with an
	// unrelated holiday configured elsewhere.
	holiday := nyTime(2026, time.January, 1, 0, 0) // unrelated Thursday holiday
	c := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{holiday}})

	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 4, 17, 0)), "ordinary Sunday reopen")
}

// --- StandardFXHolidays (issue #74) ---

func TestStandardFXHolidaysFullClosures(t *testing.T) {
	// 2029: Jan 1 is a Monday, Dec 25 a Tuesday, Dec 26 a Wednesday — all
	// ordinary weekdays, so the routine Saturday closure can't mask the
	// holiday-specific assertion the way it would for a date that
	// happens to land on a weekend.
	c := NewFXCalendar(StandardFXHolidays(2029))

	for _, d := range []time.Time{
		nyTime(2029, time.January, 1, 12, 0),
		nyTime(2029, time.December, 25, 12, 0),
		nyTime(2029, time.December, 26, 12, 0),
	} {
		assert.Equal(t, StatusHoliday, c.Status(d), "%s should be a full holiday", d)
	}
}

func TestStandardFXHolidaysPartialClosures(t *testing.T) {
	c := NewFXCalendar(StandardFXHolidays(2026))

	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.December, 24, 9, 0)))
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.December, 24, 13, 0)))
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.December, 31, 9, 0)))
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.December, 31, 13, 0)))
}

// The Dec 31 -> Jan 1 year boundary: the partial closure on Dec 31 and
// the following full holiday on Jan 1 must not leave a gap of
// unexpectedly-open time between them, and years must not bleed into
// each other.
func TestStandardFXHolidaysYearBoundary(t *testing.T) {
	c := NewFXCalendar(StandardFXHolidays(2025, 2026))

	assert.Equal(t, StatusHoliday, c.Status(nyTime(2025, time.December, 31, 16, 59)), "partial closure tail end of 2025")
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.January, 1, 0, 0)), "into the Jan 1 2026 full holiday")
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.January, 1, 16, 59)))
	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 1, 17, 0)), "first ordinary session of 2026")
}

func TestStandardFXHolidaysMultipleYearsIndependent(t *testing.T) {
	// 2030: Jan 1 is a Tuesday and Dec 25 a Wednesday, both ordinary
	// weekdays.
	c := NewFXCalendar(StandardFXHolidays(2029, 2030))

	assert.Equal(t, StatusHoliday, c.Status(nyTime(2030, time.January, 1, 12, 0)))
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2030, time.December, 25, 12, 0)))
}
