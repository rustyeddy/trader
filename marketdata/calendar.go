package marketdata

import (
	"fmt"
	"time"
)

// Status is a market's trading status at a point in time.
type Status uint8

const (
	// StatusUnknown is Status's zero value: no Calendar implementation
	// returns it. Reserving zero for an invalid/unset state means an
	// uninitialized Status can never be silently mistaken for
	// StatusOpen, the least conservative possible default for trading
	// code.
	StatusUnknown Status = iota
	// StatusOpen means the market is trading at the queried time.
	StatusOpen
	// StatusClosed means the market is not trading (a weekend or a
	// non-session hour), but the closure is routine rather than a
	// named holiday.
	StatusClosed
	// StatusHoliday means the market is not trading because the
	// queried time falls on a calendar holiday.
	StatusHoliday
)

// String returns a human-readable Status name.
func (s Status) String() string {
	switch s {
	case StatusUnknown:
		return "unknown"
	case StatusOpen:
		return "open"
	case StatusClosed:
		return "closed"
	case StatusHoliday:
		return "holiday"
	default:
		return fmt.Sprintf("Status(%d)", uint8(s))
	}
}

// Calendar aligns points in time to trading sessions and bar boundaries.
// It is a seam because Trader is expected to need more than one
// implementation: the initial FXCalendar covers spot FX's continuous
// weekly session, and exchange-traded asset classes will eventually need
// session, holiday, and early-close calendars of their own.
//
// Every method is a pure function of its time.Time argument. A Calendar
// implementation must not call time.Now internally; callers supply
// whatever time they want evaluated, keeping backtests deterministic.
type Calendar interface {
	// Status reports whether the market is open, routinely closed, or
	// on a holiday at t.
	Status(t time.Time) Status

	// Session returns the half-open trading session containing t. ok is
	// false when Status(t) is not StatusOpen.
	Session(t time.Time) (session TimeRange, ok bool)

	// Bar returns the half-open TimeRange for interval that contains t.
	// Minute and hour intervals align to the UTC clock. Day and week
	// intervals align to this Calendar's session and week boundaries
	// rather than UTC midnight.
	Bar(t time.Time, interval Interval) (TimeRange, error)
}
