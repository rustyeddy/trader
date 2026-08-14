package marketdata

import (
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/instrument"
)

// Sentinel errors returned (wrapped) by BarSet.Validate.
var (
	// ErrBarSetInstrument marks a BarSet with a zero instrument identity.
	ErrBarSetInstrument = errors.New("marketdata: bar set instrument is zero")
	// ErrBarSetInterval marks a BarSet with an unconstructed interval.
	ErrBarSetInterval = errors.New("marketdata: bar set interval is invalid")
	// ErrBarSetSpan marks a BarSet with an unset half-open span.
	ErrBarSetSpan = errors.New("marketdata: bar set span is invalid")
	// ErrBarSetBasis marks a BarSet with an unknown price basis.
	ErrBarSetBasis = errors.New("marketdata: bar set price basis is unknown")
	// ErrBarSetBarRange marks a member Bar whose Time falls outside the
	// set's span.
	ErrBarSetBarRange = errors.New("marketdata: bar set contains a bar outside its span")
	// ErrBarSetOrder marks member bars that are not strictly ordered by
	// Time (a later bar at or before an earlier one, including a
	// duplicate timestamp).
	ErrBarSetOrder = errors.New("marketdata: bar set bars are not strictly time-ordered")
)

// BarSet is a homogeneous collection of Bars: every bar shares the same
// instrument, interval, and price basis, and every bar's observed open
// falls within Span. This is the collection level of the market-data
// model, holding exactly the metadata that is guaranteed uniform across
// its bars so that metadata need not be repeated on every Bar.
//
// BarSet deliberately does not own dataset provenance, revision identity,
// or coverage/gap detail. A rebuildable canonical dataset's revision and
// build fingerprint belong to the manifest model (issue #73); which
// expected intervals are present, closed, or missing belongs to coverage
// (issue #79). BarSet references those levels; it does not absorb them.
//
// Like Bar, BarSet is a plain record with exported fields validated via
// Validate at the boundary that produces it. Span is a half-open
// [start, end): a bar whose Time equals End belongs to the next set, not
// this one.
type BarSet struct {
	Instrument instrument.ID
	Interval   Interval
	Span       TimeRange
	Basis      PriceBasis
	Bars       []Bar
}

// Validate reports whether bs is well-formed, returning a wrapped sentinel
// error for the first invariant it violates and nil when bs is valid. An
// empty Bars slice is valid: a BarSet may legitimately describe a span
// over which no bars were observed.
func (bs BarSet) Validate() error {
	if bs.Instrument.IsZero() {
		return fmt.Errorf("marketdata: bar set validate: %w", ErrBarSetInstrument)
	}
	if bs.Interval.Count() < 1 {
		return fmt.Errorf("marketdata: bar set validate: %w", ErrBarSetInterval)
	}
	switch bs.Interval.Unit() {
	case UnitMinute, UnitHour, UnitDay, UnitWeek:
	default:
		return fmt.Errorf("marketdata: bar set validate: %w", ErrBarSetInterval)
	}
	if bs.Span.start.IsZero() || bs.Span.end.IsZero() || !bs.Span.end.After(bs.Span.start) {
		return fmt.Errorf("marketdata: bar set validate: %w", ErrBarSetSpan)
	}
	if !bs.Basis.valid() {
		return fmt.Errorf("marketdata: bar set validate: basis %s: %w", bs.Basis, ErrBarSetBasis)
	}
	for i, b := range bs.Bars {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("marketdata: bar set validate: bar %d: %w", i, err)
		}
		if !bs.Span.Contains(b.Time) {
			return fmt.Errorf("marketdata: bar set validate: bar %d time %s: %w", i, b.Time, ErrBarSetBarRange)
		}
		if i > 0 && !b.Time.After(bs.Bars[i-1].Time) {
			return fmt.Errorf("marketdata: bar set validate: bar %d time %s not after bar %d time %s: %w",
				i, b.Time, i-1, bs.Bars[i-1].Time, ErrBarSetOrder)
		}
	}
	return nil
}

// Len returns the number of bars in the set.
func (bs BarSet) Len() int {
	return len(bs.Bars)
}
