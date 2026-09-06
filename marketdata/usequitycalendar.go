package marketdata

import (
	"fmt"
	"time"
)

// USEquityCalendar is a Calendar for the regular U.S. equity market
// session (NYSE/Nasdaq): open Monday through Friday, 9:30-16:00
// America/New_York, closed on weekends and a fixed holiday list, with
// no built-in awareness of pre-market or after-hours trading (issue
// #296, EQ-03 — extended hours stay deferred, per that issue's own
// acceptance criteria). Status and Session are honest about real
// regular-session clock hours: an instant outside 9:30-16:00
// America/New_York on an otherwise-open trading day reports
// StatusClosed, not StatusOpen (PR #310 review) — this calendar does
// not claim an overnight timestamp belongs to the regular session
// merely because its calendar date is a trading day.
//
// # D1 bar-label anchor: midnight UTC, decoupled from real session hours
//
// Bar(t, D1) returns [midnight UTC, next midnight UTC) — not the real
// 9:30-16:00 America/New_York session Status/Session report. This is a
// deliberate, documented split between two different concepts, not an
// oversight:
//
//   - Bar's job is to reproduce the exact instant Stooq's own daily
//     bars are stored at, so Manager.Coverage's present-partition
//     lookup (keyed by the stored Bar.Time) finds them. Stooq's dates
//     are parsed via time.Parse("2006-01-02", ...) in
//     marketdata/internal/provider/stooq/reader.go, which Go resolves
//     to midnight UTC with no location conversion — there is no
//     timezone information in Stooq's native "YYYY-MM-DD" dates at
//     all. Anchoring Bar to the exchange's real local midnight (or
//     session open) instead would make every real Stooq bar
//     misaligned by construction, reproducing exactly the original bug
//     this issue exists to fix.
//   - Status/Session's job is to honestly answer "does this instant
//     belong to the regular trading session" for an arbitrary
//     wall-clock timestamp — issue #296's own explicit acceptance
//     criterion — which midnight UTC almost never does (it is roughly
//     7-8pm the previous evening in New York).
//
// A D1 bar's own literal 24-hour span therefore does not coincide with
// when Status(bar span's start) is honestly Open — unlike FXCalendar,
// whose D1 boundary (the FX daily rollover) is itself a real
// session-open/close instant. Classifying a whole labeled trading day
// as open or closed therefore cannot be done by sampling Status/Session
// at the bar's own literal endpoints the way ClassifyInterval's shared
// uniformStatus helper does for FXCalendar — see ClassifyBarSpan below,
// which is exactly the mechanism this calendar uses instead.
type USEquityCalendar struct {
	holidays map[dateKey]struct{}
	halfDays map[dateKey]time.Duration
}

// HalfDay names a date on which the regular session closes early
// (issue #296's own "early-close/half-day behavior" scope item), and
// the local close time as an offset from that date's midnight. Unlike
// a full holiday, a HalfDay is still an open trading day — Status and
// Session both honor CloseAt directly (the session simply ends early),
// and ClassifyBarSpan still reports the whole labeled day as Open,
// matching Stooq's own single, ordinary D1 bar for that date.
type HalfDay struct {
	// Date names, via its literal Year/Month/Day fields, the calendar
	// date that closes early — the same literal-field convention
	// FXCalendar's Holidays/PartialClosure.Date already use.
	Date time.Time
	// CloseAt is the local close time as an offset from Date's own
	// midnight (America/New_York) — for example 13*time.Hour for a
	// 1:00pm close.
	CloseAt time.Duration
}

// USEquityCalendarParams configures a new USEquityCalendar.
type USEquityCalendarParams struct {
	// Holidays names calendar dates on which the market is fully
	// closed. Each entry's literal Year/Month/Day fields are read
	// verbatim, regardless of its own Location or time-of-day — the
	// same convention FXCalendarParams.Holidays uses.
	Holidays []time.Time
	// HalfDays names dates that close early. See HalfDay.
	HalfDays []HalfDay
}

// NewUSEquityCalendar returns a USEquityCalendar configured with
// params.
func NewUSEquityCalendar(params USEquityCalendarParams) *USEquityCalendar {
	holidays := make(map[dateKey]struct{}, len(params.Holidays))
	for _, h := range params.Holidays {
		holidays[literalDateKey(h)] = struct{}{}
	}
	halfDays := make(map[dateKey]time.Duration, len(params.HalfDays))
	for _, hd := range params.HalfDays {
		halfDays[literalDateKey(hd.Date)] = hd.CloseAt
	}
	return &USEquityCalendar{holidays: holidays, halfDays: halfDays}
}

var _ Calendar = (*USEquityCalendar)(nil)
var _ BarSpanClassifier = (*USEquityCalendar)(nil)

// utcDateKey returns t's calendar date in UTC — the civil date Bar's
// own midnight-UTC day anchor uses throughout, independent of what
// Location t itself carries.
func utcDateKey(t time.Time) dateKey {
	u := t.UTC()
	y, m, d := u.Date()
	return dateKey{year: y, month: m, day: d}
}

// dayStart returns the midnight-UTC instant starting the calendar date
// containing t.
func dayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// regularOpen and regularClose are the standard NYSE/Nasdaq regular
// trading session hours, as an offset from local midnight
// (America/New_York).
const (
	regularOpen  = 9*time.Hour + 30*time.Minute
	regularClose = 16 * time.Hour
)

// classify is Status and Session's single shared implementation,
// mirroring FXCalendar's own classify method exactly: computing both
// together in one place is what guarantees Status and Session can
// never disagree about which instants are open. It answers honestly,
// in America/New_York civil time, whether t falls within the regular
// trading session on its own calendar date — never merely whether that
// date is a trading day in the abstract (see USEquityCalendar's own
// doc comment for why that distinction matters, and ClassifyBarSpan
// for the whole-day-label question this method deliberately does not
// answer).
func (c *USEquityCalendar) classify(t time.Time) (Status, TimeRange) {
	nyT := t.In(newYorkLocation)
	switch nyT.Weekday() {
	case time.Saturday, time.Sunday:
		return StatusClosed, TimeRange{}
	}
	key := literalDateKey(nyT)
	if _, holiday := c.holidays[key]; holiday {
		return StatusHoliday, TimeRange{}
	}

	y, m, d := nyT.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, newYorkLocation)
	open := midnight.Add(regularOpen)
	closeAt := regularClose
	if half, ok := c.halfDays[key]; ok {
		closeAt = half
	}
	closeTime := midnight.Add(closeAt)

	if nyT.Before(open) || !nyT.Before(closeTime) {
		return StatusClosed, TimeRange{}
	}
	window, err := NewTimeRange(open, closeTime)
	if err != nil {
		// Unreachable: open is always strictly before closeTime for any
		// configured HalfDay (a close time after 9:30am) or the regular
		// 16:00 close.
		panic(fmt.Sprintf("marketdata: unreachable: %v", err))
	}
	return StatusOpen, window
}

// Status implements Calendar: open only during the regular trading
// session (9:30-16:00 America/New_York, truncated by a configured
// HalfDay) on a weekday that is not a configured holiday. An instant
// outside those clock hours — including an ordinary weeknight, or the
// early morning before the opening bell — reports StatusClosed, even
// on an otherwise-open trading day (PR #310 review: this must not
// silently claim a 3am or 10pm timestamp belongs to the regular
// session).
func (c *USEquityCalendar) Status(t time.Time) Status {
	status, _ := c.classify(t)
	return status
}

// Session implements Calendar: the real regular-session TimeRange
// (9:30-16:00 America/New_York, truncated on a half day) containing t,
// or ok=false when t does not fall within one — including outside
// business hours on an otherwise-open trading day. See classify.
func (c *USEquityCalendar) Session(t time.Time) (TimeRange, bool) {
	status, window := c.classify(t)
	if status != StatusOpen {
		return TimeRange{}, false
	}
	return window, true
}

// RegularSessionEnd returns the real regular-session close instant for
// the trading day containing t (its own America/New_York calendar
// date), and whether that date is a trading day at all. Unlike
// Session, RegularSessionEnd answers this regardless of what clock
// time t itself falls at — it is the close-time lookup a live-fetch
// provider (Alpaca, issue #297) needs to determine whether a given
// trading day's own daily bar has actually finished forming yet,
// which Session cannot answer directly: Session(t) reports ok=false
// for any out-of-session t, including midnight UTC — the very instant
// a caller most needs to ask "has today's session ended?" from (PR
// #312 review: syncOneAlpaca previously assumed every bar Alpaca could
// return for the current calendar day was already complete, with
// nothing checking whether that day's regular session had actually
// closed yet).
func (c *USEquityCalendar) RegularSessionEnd(t time.Time) (closeAt time.Time, isTradingDay bool) {
	nyT := t.In(newYorkLocation)
	switch nyT.Weekday() {
	case time.Saturday, time.Sunday:
		return time.Time{}, false
	}
	key := literalDateKey(nyT)
	if _, holiday := c.holidays[key]; holiday {
		return time.Time{}, false
	}
	y, m, d := nyT.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, newYorkLocation)
	closeOffset := regularClose
	if half, ok := c.halfDays[key]; ok {
		closeOffset = half
	}
	return midnight.Add(closeOffset), true
}

// Bar implements Calendar. Only D1 is supported today — ADR-047
// explicitly scopes Phase 1's equity Calendar need to daily bars only,
// since Stooq (Phase 1's only equity data source) has no intraday
// history; a real M1/H1 alignment rule is deferred until an intraday
// equity provider (Alpaca) motivates one.
func (c *USEquityCalendar) Bar(t time.Time, interval Interval) (TimeRange, error) {
	if err := interval.validate(); err != nil {
		return TimeRange{}, err
	}
	if interval.Unit() != UnitDay || interval.Count() != 1 {
		return TimeRange{}, fmt.Errorf(
			"marketdata: USEquityCalendar only aligns D1 bars today, got %s", interval)
	}
	start := dayStart(t)
	return NewTimeRange(start, start.AddDate(0, 0, 1))
}

// ClassifyBarSpan implements the optional BarSpanClassifier capability
// (interval_state.go). It answers a different question than
// Status/Session: not "is this literal instant within the regular
// session," but "does span — a whole Bar(t, D1)-shaped labeled trading
// day — represent a legitimate single open day, a holiday, or a
// weekend closure." span's own literal endpoints (midnight UTC) fall
// outside real trading hours by construction (see USEquityCalendar's
// own doc comment), so this deliberately does not sample Status at
// them; it classifies the labeled UTC calendar date itself, the same
// weekday/holiday determination classify uses, without the regular-
// session clock-hour gate.
//
// A HalfDay is reported as StatusOpen here — it is still one ordinary
// labeled trading day with one ordinary D1 bar, just a shorter regular
// session (which Status/Session already reflect for callers that need
// clock-hour precision).
func (c *USEquityCalendar) ClassifyBarSpan(span TimeRange) (Status, error) {
	date := span.Start()
	switch date.UTC().Weekday() {
	case time.Saturday, time.Sunday:
		return StatusClosed, nil
	}
	if _, holiday := c.holidays[utcDateKey(date)]; holiday {
		return StatusHoliday, nil
	}
	return StatusOpen, nil
}

// IsHalfDay reports whether t's calendar date is a configured early
// closing day, and, if so, the local close time as an offset from that
// date's own midnight. Status and Session already honor this directly
// (see classify); IsHalfDay exists as a direct, explicit accessor for
// a caller that wants to know a date is a half day without separately
// probing Status right at the boundary (issue #296's own "at least
// representative early-close dates are tested" requirement).
func (c *USEquityCalendar) IsHalfDay(t time.Time) (closeAt time.Duration, ok bool) {
	closeAt, ok = c.halfDays[literalDateKey(t.In(newYorkLocation))]
	return closeAt, ok
}
