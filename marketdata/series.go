package marketdata

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidSeries reports a Series that is not what its doc says: unnamed, a bar
// of another instrument, an invalid bar, or bars not strictly ascending.
var ErrInvalidSeries = errors.New("marketdata: invalid series")

// Series is one instrument's bars at one interval from one source, in time
// order: what a consumer replays. It is the shared container that readers return
// and the engine consumes, so no consumer needs a type of its own for it.
//
// The bars are strictly ascending by opening Time and strictly ascending by
// AvailableAt, which is what delivering several instruments in availability order
// needs. Each carries the series' Instrument. A series may have gaps (a weekend,
// a month the caller allowed to be missing): nothing is filled, interpolated or
// carried forward.
type Series struct {
	// Source is the provider the bars came from (for example "oanda").
	Source string
	// Instrument is the symbol as spelled in the data path (for example "EURUSD").
	Instrument string
	// Interval is the lower-case interval token, such as "h1" or "d1".
	Interval string
	// Bars are in time order.
	Bars []Bar
}

// Validate reports ErrInvalidSeries unless the series is named, every bar is
// valid (Bar.Validate) and carries the series' Instrument, and the bars are
// strictly ascending by Time and by AvailableAt. An empty series is valid: it is a
// range with no bars.
func (s Series) Validate() error {
	switch {
	case s.Source == "":
		return fmt.Errorf("%w: no source", ErrInvalidSeries)
	case s.Instrument == "":
		return fmt.Errorf("%w: no instrument", ErrInvalidSeries)
	case s.Interval == "":
		return fmt.Errorf("%w: no interval", ErrInvalidSeries)
	}
	var prev Bar
	for i, b := range s.Bars {
		at := b.Time.UTC().Format(time.RFC3339)
		if err := b.Validate(); err != nil {
			return fmt.Errorf("%w: bar %s: %w", ErrInvalidSeries, at, err)
		}
		if b.Instrument != s.Instrument {
			return fmt.Errorf("%w: bar %s is of %q, the series is of %q", ErrInvalidSeries, at, b.Instrument, s.Instrument)
		}
		if i > 0 {
			if !b.Time.After(prev.Time) {
				return fmt.Errorf("%w: bar %s is not after %s", ErrInvalidSeries, at, prev.Time.UTC().Format(time.RFC3339))
			}
			if !b.AvailableAt.After(prev.AvailableAt) {
				return fmt.Errorf("%w: bar %s is not available after the one before it", ErrInvalidSeries, at)
			}
		}
		prev = b
	}
	return nil
}
