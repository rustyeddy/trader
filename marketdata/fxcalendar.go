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
// week. It supports an optional fixed holiday list and partial-closure
// list; no holiday data ships with Trader itself — see StandardFXHolidays
// for Trader's own accepted M2 rule set (issue #74, ADR-020).
type FXCalendar struct {
	holidays        map[dateKey]struct{}
	partialClosures map[dateKey]time.Duration
}

// dateKey identifies a civil date in New York's calendar, independent of
// time-of-day, for holiday matching.
type dateKey struct {
	year  int
	month time.Month
	day   int
}

// PartialClosure names a session that is truncated early rather than
// closed for its entire span: the session ending on Date remains open
// from its normal Sunday/weekday open through Date at New York midnight
// plus From, and is closed (StatusHoliday) from that instant through the
// session's normal 17:00 New York end. Christmas Eve and New Year's Eve,
// both closing at 13:00 New York time, are Trader's two M2 examples (see
// StandardFXHolidays).
type PartialClosure struct {
	// Date names, via its literal Year/Month/Day fields — the same
	// convention Holidays uses — the New York civil date on which the
	// session ending on Date is truncated.
	Date time.Time
	// From is the offset from New York midnight on Date at which trading
	// stops for the session ending on Date. It is ordinarily well within
	// [0, 17h): a value at or beyond 17h would never actually truncate
	// anything, since the session already ends at 17:00.
	From time.Duration
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

	// PartialClosures names sessions that end early rather than not
	// opening at all. See PartialClosure.
	PartialClosures []PartialClosure
}

// NewFXCalendar returns an FXCalendar configured with params.
func NewFXCalendar(params FXCalendarParams) *FXCalendar {
	holidays := make(map[dateKey]struct{}, len(params.Holidays))
	for _, h := range params.Holidays {
		holidays[literalDateKey(h)] = struct{}{}
	}
	partial := make(map[dateKey]time.Duration, len(params.PartialClosures))
	for _, pc := range params.PartialClosures {
		partial[literalDateKey(pc.Date)] = pc.From
	}
	return &FXCalendar{holidays: holidays, partialClosures: partial}
}

var _ Calendar = (*FXCalendar)(nil)

// Status implements Calendar. Status and Session are never computed
// independently — both are thin wrappers over classify — so they cannot
// disagree: every instant a returned Session contains reports the same
// Status that produced it.
func (c *FXCalendar) Status(t time.Time) Status {
	status, _ := c.classify(t)
	return status
}

// Session implements Calendar. When a partial closure or a holiday-Sunday
// head truncation applies to t's session, the returned range is the
// truncated open window, not the nominal rollover-to-rollover span — see
// classify.
func (c *FXCalendar) Session(t time.Time) (TimeRange, bool) {
	status, window := c.classify(t)
	if status != StatusOpen {
		return TimeRange{}, false
	}
	return window, true
}

// classify is Status and Session's single shared implementation. It
// returns t's Status, and — only when that Status is StatusOpen — the
// actual open trading window containing t: the nominal
// [dayStart, dayStart+1day) rollover-to-rollover session, truncated at
// its start when the session opened on a holiday Sunday (see the Sunday
// case below) and truncated at its end when a PartialClosure applies.
// When Status is not StatusOpen, the returned TimeRange is the zero
// value and must not be used — Session reports ok=false in that case
// without inspecting it.
//
// Computing both together in one place, rather than letting Status and
// Session each independently decide where a session's boundaries fall,
// is what guarantees they can never disagree about which instants are
// open.
func (c *FXCalendar) classify(t time.Time) (Status, TimeRange) {
	nyT := t.In(newYorkLocation)
	switch nyT.Weekday() {
	case time.Saturday:
		return StatusClosed, TimeRange{}
	case time.Friday:
		if !nyT.Before(rolloverOn(nyT)) {
			return StatusClosed, TimeRange{}
		}
	case time.Sunday:
		if nyT.Before(rolloverOn(nyT)) {
			return StatusClosed, TimeRange{}
		}
		// At or after Sunday's rollover, the week would normally reopen.
		// It doesn't when this Sunday is itself a full holiday (Jan 1 or
		// Dec 25 landing on a Sunday): the session that would open
		// tonight and end the following Monday isn't otherwise caught by
		// the ending-date check below, since an ordinary Monday isn't a
		// holiday date in its own right. This case is reached only while
		// t's own New York civil date is still that Sunday — once the
		// date rolls to Monday, nyT.Weekday() no longer matches here, so
		// the closed head of the session is exactly [Sunday 17:00,
		// Monday 00:00 NY), matching the reopening-at-midnight semantics
		// below.
		if _, holiday := c.holidays[dateKeyOf(nyT)]; holiday {
			return StatusHoliday, TimeRange{}
		}
	}

	start := c.dayStart(t)
	end := start.AddDate(0, 0, 1)
	endKey := dateKeyOf(end)

	if _, holiday := c.holidays[endKey]; holiday {
		return StatusHoliday, TimeRange{}
	}
	if from, ok := c.partialClosures[endKey]; ok {
		// end already carries newYorkLocation (built from rolloverOn via
		// dayStart), so its own Y/M/D give the correct civil date for
		// the truncation threshold without a further location conversion.
		midnight := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, newYorkLocation)
		threshold := midnight.Add(from)
		if !t.Before(threshold) {
			return StatusHoliday, TimeRange{}
		}
		if threshold.Before(end) {
			end = threshold
		}
	}
	if start.Weekday() == time.Sunday {
		// Reaching this point means the Sunday-holiday check above did
		// not return Holiday for t, so t is already past that session's
		// closed head (see the comment there). Report the window's
		// actual open start as the following New York midnight, not the
		// nominal Sunday 17:00 rollover.
		if _, holiday := c.holidays[dateKeyOf(start)]; holiday {
			reopen := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, newYorkLocation).AddDate(0, 0, 1)
			if reopen.After(start) {
				start = reopen
			}
		}
	}

	window, err := NewTimeRange(start, end)
	if err != nil {
		// Unreachable: start < end always holds here — the Sunday head
		// truncation only ever moves start forward to a point still
		// strictly before the nominal end, and the partial-closure tail
		// truncation only ever moves end backward to threshold, which is
		// only applied when threshold is itself before end.
		panic(fmt.Sprintf("marketdata: unreachable: %v", err))
	}
	return StatusOpen, window
}

// Bar implements Calendar.
func (c *FXCalendar) Bar(t time.Time, interval Interval) (TimeRange, error) {
	// Reject a malformed interval — including the zero Interval, which
	// carries UnitMinute with a zero count — before it reaches the
	// unit-specific alignment below, where a zero count would otherwise
	// surface as a confusing empty-range error rather than an interval one.
	if err := interval.validate(); err != nil {
		return TimeRange{}, err
	}
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
