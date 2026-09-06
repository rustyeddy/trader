package marketdata

import (
	"fmt"
	"time"
)

// USEquityCalendar is a Calendar for the regular U.S. equity market
// session (NYSE/Nasdaq): open Monday through Friday, closed on
// weekends and a fixed holiday list, with no built-in awareness of
// pre-market or after-hours trading (issue #296, EQ-03 — extended
// hours stay deferred, per that issue's own acceptance criteria).
//
// # Day-boundary anchor: midnight UTC, not a "real" exchange-local day
//
// Unlike FXCalendar, whose D1 boundary is the FX daily rollover (17:00
// America/New_York, ADR-021), USEquityCalendar's day boundary is
// midnight UTC. This is a deliberate choice, not an oversight: this
// calendar exists specifically to serve Stooq-sourced canonical equity
// data (issue #303, EQ-03A), and Stooq's own daily bars are parsed via
// time.Parse("2006-01-02", ...) in
// marketdata/internal/provider/stooq/reader.go, which Go's time.Parse
// resolves to midnight UTC with no location conversion — there is no
// timezone information in Stooq's native "YYYY-MM-DD" dates at all.
// Anchoring USEquityCalendar's D1 boundary to the exchange's real local
// midnight (or session open) instead would make every real Stooq bar
// misaligned by construction, reproducing exactly the bug this issue
// exists to fix (see TestStooqCoverage_HolidayGapPendingEquityCalendar's
// history, superseded by this type). There is no requirement that a day
// boundary be timezone-authentic when the entire point of this
// calendar, for now, is agreeing with what is actually stored.
//
// # Session equals the whole day, not real trading-session clock hours
//
// Session(t) for an open day returns the same [midnight UTC, next
// midnight UTC) span Bar(t, D1) does — not the real 9:30-16:00
// America/New_York regular session. This is required, not merely
// convenient: ClassifyInterval (interval_state.go) rejects a bar span
// with ErrIntervalStraddlesBoundary unless, whenever Status is
// StatusOpen, Session fully contains that span. A ~24h D1 bar span
// cannot be contained by a real ~6.5h trading session, so returning
// literal clock hours here would make every single D1 classification
// fail instead of merely being imprecise. "Session," at this
// calendar's current D1-only granularity, therefore means "the trading
// day as a whole," not "opening bell to closing bell." Real intraday
// session-hours accuracy (9:30 open, 16:00 close, a truncated close on
// a half day) is a named, deferred refinement for when equity M1/H1
// support (Alpaca-driven) actually needs to distinguish pre-market,
// regular, and after-hours data at sub-day granularity — see IsHalfDay
// for what this calendar already tracks in preparation for that, without
// yet acting on it.
type USEquityCalendar struct {
	holidays map[dateKey]struct{}
	halfDays map[dateKey]time.Duration
}

// HalfDay names a date on which the regular session closes early
// (issue #296's own "early-close/half-day behavior" scope item), and
// the local close time as an offset from that date's midnight. Unlike
// PartialClosure (FXCalendar's analogous concept), a HalfDay does not
// yet truncate Session or change Status — see USEquityCalendar's own
// doc comment for why — it is informational data IsHalfDay exposes for
// a caller (or a future intraday-aware refinement of this type) to
// consult directly.
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

// utcDateKey returns t's calendar date in UTC — the civil date
// USEquityCalendar's own midnight-UTC day anchor uses throughout,
// independent of what Location t itself carries.
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

// Status implements Calendar: open Monday-Friday except a configured
// holiday, closed on weekends and holidays.
func (c *USEquityCalendar) Status(t time.Time) Status {
	u := t.UTC()
	switch u.Weekday() {
	case time.Saturday, time.Sunday:
		return StatusClosed
	}
	if _, holiday := c.holidays[utcDateKey(u)]; holiday {
		return StatusHoliday
	}
	return StatusOpen
}

// Session implements Calendar. See USEquityCalendar's own doc comment
// for why this returns the whole UTC calendar day, not real trading
// hours.
func (c *USEquityCalendar) Session(t time.Time) (TimeRange, bool) {
	if c.Status(t) != StatusOpen {
		return TimeRange{}, false
	}
	start := dayStart(t)
	window, err := NewTimeRange(start, start.AddDate(0, 0, 1))
	if err != nil {
		// Unreachable: start is always strictly before start+1 day.
		panic(fmt.Sprintf("marketdata: unreachable: %v", err))
	}
	return window, true
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

// IsHalfDay reports whether t's calendar date is a configured early
// closing day, and, if so, the local close time as an offset from that
// date's own midnight. It is purely informational at this calendar's
// current D1-only granularity — see USEquityCalendar's own doc comment
// — but exists so a half day is at least distinguishable from an
// ordinary trading day, and from a full closure, rather than silently
// unrepresented (issue #296's own "early-close/half-day behavior" and
// "at least representative early-close dates are tested" requirements).
func (c *USEquityCalendar) IsHalfDay(t time.Time) (closeAt time.Duration, ok bool) {
	closeAt, ok = c.halfDays[utcDateKey(t)]
	return closeAt, ok
}
