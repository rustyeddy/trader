package marketdata

import (
	"fmt"
	"time"

	// tzdata embeds the IANA time zone database in the binary so
	// America/New_York (and its DST transition rules) resolve
	// identically regardless of the host's installed tzdata, keeping
	// FXCalendar's DST-dependent boundaries deterministic.
	_ "time/tzdata"
)

// newYorkLocation is loaded once at package init from the embedded
// tzdata database. Loading it can only fail if the embedded database
// itself is corrupt, which would indicate a broken Go toolchain rather
// than a recoverable runtime condition.
var newYorkLocation = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(fmt.Sprintf("marketdata: failed to load America/New_York: %v", err))
	}
	return loc
}()

// fxRolloverHour is the local New York clock hour at which the FX
// trading day and week roll over: 17:00 (5pm), unaffected by whether
// New York is currently observing daylight saving time — time.Date
// resolves 17:00 to the correct UTC instant for whichever offset
// applies on that date.
const fxRolloverHour = 17

// FXCalendar is a Calendar for spot FX's continuous weekly session: open
// Sunday 17:00 New York time, closed Friday 17:00 through Sunday 17:00,
// with a daily rollover at 17:00 New York time every other day of the
// week. It supports an optional fixed holiday list; no holiday data
// ships with Trader itself.
type FXCalendar struct {
	holidays map[dateKey]struct{}
}

// dateKey identifies a civil date in New York's calendar, independent of
// time-of-day, for holiday matching.
type dateKey struct {
	year  int
	month time.Month
	day   int
}

// FXCalendarParams configures a new FXCalendar.
type FXCalendarParams struct {
	// Holidays names New York civil dates on which the market is closed
	// for the entire rollover-to-rollover trading session that ends on
	// that date (see Session): the Year/Month/Day fields of each entry
	// are taken literally, regardless of its Location or time-of-day —
	// construct entries with time.Date(y, m, d, 0, 0, 0, 0, loc) using
	// whatever Location is convenient, since only the calendar fields
	// are read.
	Holidays []time.Time
}

// NewFXCalendar returns an FXCalendar configured with params.
func NewFXCalendar(params FXCalendarParams) *FXCalendar {
	holidays := make(map[dateKey]struct{}, len(params.Holidays))
	for _, h := range params.Holidays {
		holidays[literalDateKey(h)] = struct{}{}
	}
	return &FXCalendar{holidays: holidays}
}

var _ Calendar = (*FXCalendar)(nil)

// Status implements Calendar. Status and Session agree with each other:
// both group time by the same rollover-to-rollover trading session
// (dayStart to dayStart+1day), labeled by the New York civil date on
// which that session closes, so every instant a returned Session
// contains reports the same Status.
func (c *FXCalendar) Status(t time.Time) Status {
	nyT := t.In(newYorkLocation)
	switch nyT.Weekday() {
	case time.Saturday:
		return StatusClosed
	case time.Friday:
		if !nyT.Before(rolloverOn(nyT)) {
			return StatusClosed
		}
	case time.Sunday:
		if nyT.Before(rolloverOn(nyT)) {
			return StatusClosed
		}
	}
	sessionEnd := c.dayStart(t).AddDate(0, 0, 1)
	if _, holiday := c.holidays[dateKeyOf(sessionEnd)]; holiday {
		return StatusHoliday
	}
	return StatusOpen
}

// Session implements Calendar.
func (c *FXCalendar) Session(t time.Time) (TimeRange, bool) {
	if c.Status(t) != StatusOpen {
		return TimeRange{}, false
	}
	start := c.dayStart(t)
	r, err := NewTimeRange(start, start.AddDate(0, 0, 1))
	if err != nil {
		// start.AddDate(0, 0, 1) is always strictly after start.
		panic(fmt.Sprintf("marketdata: unreachable: %v", err))
	}
	return r, true
}

// Bar implements Calendar.
func (c *FXCalendar) Bar(t time.Time, interval Interval) (TimeRange, error) {
	switch interval.Unit() {
	case UnitMinute:
		return utcBar(t, time.Duration(interval.Count())*time.Minute)
	case UnitHour:
		return utcBar(t, time.Duration(interval.Count())*time.Hour)
	case UnitDay:
		if interval.Count() != 1 {
			return TimeRange{}, fmt.Errorf("marketdata: FXCalendar only aligns single-day bars, got count %d", interval.Count())
		}
		start := c.dayStart(t)
		return NewTimeRange(start, start.AddDate(0, 0, 1))
	case UnitWeek:
		if interval.Count() != 1 {
			return TimeRange{}, fmt.Errorf("marketdata: FXCalendar only aligns single-week bars, got count %d", interval.Count())
		}
		start := c.weekStart(t)
		return NewTimeRange(start, start.AddDate(0, 0, 7))
	default:
		return TimeRange{}, fmt.Errorf("marketdata: invalid interval unit %v", interval.Unit())
	}
}

// dayStart returns the most recent 17:00 New York rollover at or before
// t: the start of the trading day containing t, independent of whether
// that day is open, closed, or a holiday.
func (c *FXCalendar) dayStart(t time.Time) time.Time {
	nyT := t.In(newYorkLocation)
	rollover := rolloverOn(nyT)
	if nyT.Before(rollover) {
		rollover = rollover.AddDate(0, 0, -1)
	}
	return rollover
}

// weekStart returns the Sunday 17:00 New York rollover that opens the
// trading week containing t.
func (c *FXCalendar) weekStart(t time.Time) time.Time {
	d := c.dayStart(t)
	for d.Weekday() != time.Sunday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// rolloverOn returns the 17:00 New York instant on nyT's own calendar
// date. nyT must already be in newYorkLocation.
func rolloverOn(nyT time.Time) time.Time {
	return time.Date(nyT.Year(), nyT.Month(), nyT.Day(), fxRolloverHour, 0, 0, 0, newYorkLocation)
}

// dateKeyOf returns t's New York civil date, converting t to
// newYorkLocation first. Used for query times, which may be expressed in
// any location.
func dateKeyOf(t time.Time) dateKey {
	nyT := t.In(newYorkLocation)
	y, m, d := nyT.Date()
	return dateKey{year: y, month: m, day: d}
}

// literalDateKey returns h's Year/Month/Day fields as given, with no
// Location conversion. Used for FXCalendarParams.Holidays entries, whose
// calendar fields are documented as the literal holiday date.
func literalDateKey(h time.Time) dateKey {
	y, m, d := h.Date()
	return dateKey{year: y, month: m, day: d}
}

// utcBar truncates t to the most recent multiple of duration since the
// Go zero time. The zero time falls at 00:00:00 UTC, so this always
// aligns to the UTC clock rather than to an arbitrary epoch. For
// durations that evenly divide 24 hours — as every predefined Interval
// here does — the resulting boundaries repeat at the same clock time
// every day (00:00, 04:00, ... for four-hour bars, for example).
func utcBar(t time.Time, duration time.Duration) (TimeRange, error) {
	start := t.UTC().Truncate(duration)
	return NewTimeRange(start, start.Add(duration))
}
