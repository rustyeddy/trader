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

func TestFXCalendarHoliday(t *testing.T) {
	holiday := nyTime(2026, time.January, 1, 0, 0) // New Year's Day, a Thursday
	c := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{holiday}})

	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.January, 1, 10, 0)))
	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.January, 1, 23, 59)))

	_, ok := c.Session(nyTime(2026, time.January, 1, 10, 0))
	assert.False(t, ok, "holiday has no session")

	assert.Equal(t, StatusOpen, c.Status(nyTime(2026, time.January, 2, 10, 0)), "the following day is unaffected")
}

func TestFXCalendarHolidayIgnoresTimeOfDayAndLocation(t *testing.T) {
	// Supplied in UTC, at a time-of-day that would fall on a different
	// New York calendar date; only the New York civil date must match.
	holiday := time.Date(2026, time.July, 4, 23, 30, 0, 0, time.UTC) // 19:30 NY on July 4
	c := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{holiday}})

	assert.Equal(t, StatusHoliday, c.Status(nyTime(2026, time.July, 4, 8, 0)))
}

func TestFXCalendarImplementsCalendar(t *testing.T) {
	var _ Calendar = NewFXCalendar(FXCalendarParams{})
}
