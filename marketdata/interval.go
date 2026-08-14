package marketdata

import "fmt"

// Unit is the calendar unit an Interval counts in.
type Unit uint8

const (
	// UnitMinute counts elapsed minutes, aligned to the UTC clock.
	UnitMinute Unit = iota
	// UnitHour counts elapsed hours, aligned to the UTC clock.
	UnitHour
	// UnitDay counts trading days, aligned to a Calendar's session
	// rollover rather than UTC midnight.
	UnitDay
	// UnitWeek counts trading weeks, aligned to a Calendar's week open.
	UnitWeek
)

// String returns u's short display name: "m", "h", "d", or "w". It is
// never parsed back into a Unit; see Interval.String.
func (u Unit) String() string {
	switch u {
	case UnitMinute:
		return "m"
	case UnitHour:
		return "h"
	case UnitDay:
		return "d"
	case UnitWeek:
		return "w"
	default:
		return fmt.Sprintf("Unit(%d)", uint8(u))
	}
}

// Interval is a typed bar interval: a Unit and a count, for example one
// hour or four hours. Interval values are constructed from typed
// unit/count pairs — never parsed from provider-native strings such as
// "H4" — so core code cannot silently misinterpret a malformed interval
// string. Interval's own String method exists only for display and
// logging; it is documented as one-directional.
type Interval struct {
	unit  Unit
	count int
}

// NewInterval returns an Interval for unit and count. It reports an error
// if count is less than 1 or unit is not one of the defined Unit values.
func NewInterval(unit Unit, count int) (Interval, error) {
	i := Interval{unit: unit, count: count}
	if err := i.validate(); err != nil {
		return Interval{}, err
	}
	return i, nil
}

// validate reports whether i's unit and count are well-formed, returning
// a descriptive error otherwise. It is the single source of interval
// validity: NewInterval builds through it, and Valid exposes it. Because
// unexported fields cannot stop an out-of-package caller from producing
// the zero Interval (unit UnitMinute, count 0), which never passes
// through NewInterval, consumers that receive an Interval they did not
// construct should validate it at the point of use.
func (i Interval) validate() error {
	switch i.unit {
	case UnitMinute, UnitHour, UnitDay, UnitWeek:
	default:
		return fmt.Errorf("marketdata: invalid interval unit %v", i.unit)
	}
	if i.count < 1 {
		return fmt.Errorf("marketdata: invalid interval count %d: must be at least 1", i.count)
	}
	return nil
}

// Valid reports whether i is a well-formed interval: a defined Unit with a
// count of at least 1. The zero Interval is not valid. Consumers that
// accept an Interval they did not construct cannot assume it came from
// NewInterval and should check Valid before relying on it.
func (i Interval) Valid() bool {
	return i.validate() == nil
}

// Unit returns i's calendar unit.
func (i Interval) Unit() Unit {
	return i.unit
}

// Count returns i's unit count.
func (i Interval) Count() int {
	return i.count
}

// String returns a short display form such as "M1", "H4", "D1", or "W1".
// It is display-only: Trader never parses an Interval back out of this
// string, so it carries no obligation to round-trip.
func (i Interval) String() string {
	unitLetter := "?"
	switch i.unit {
	case UnitMinute:
		unitLetter = "M"
	case UnitHour:
		unitLetter = "H"
	case UnitDay:
		unitLetter = "D"
	case UnitWeek:
		unitLetter = "W"
	}
	return fmt.Sprintf("%s%d", unitLetter, i.count)
}

// Predefined intervals covering the common cases: one minute, one hour,
// four hours, one trading day, and one trading week. Callers needing other
// combinations use NewInterval directly.
var (
	M1 = Interval{unit: UnitMinute, count: 1}
	H1 = Interval{unit: UnitHour, count: 1}
	H4 = Interval{unit: UnitHour, count: 4}
	D1 = Interval{unit: UnitDay, count: 1}
	W1 = Interval{unit: UnitWeek, count: 1}
)
