package marketdata

import (
	"errors"
	"fmt"
	"time"
)

// IntervalState classifies why a specific expected bar interval within a
// query range does, or does not, have canonical data, and whether that
// answer is even settled yet (issue #74, ADR-020). It replaces the old
// dense, zero-filled representation: absence plus this state is how a
// closed market or a genuine gap is represented, never a dummy zero-value
// Bar.
//
// IntervalState is the vocabulary a coverage engine reports per interval;
// walking a real query range and producing these values for it is issue
// #79's job, not this package's. ClassifyInterval below classifies one
// interval at a time from facts the caller already has.
type IntervalState uint8

const (
	// IntervalStateUnknown is IntervalState's zero value. No function in
	// this package returns it for a valid input, so an uninitialized
	// IntervalState can never be silently mistaken for a settled answer.
	IntervalStateUnknown IntervalState = iota
	// IntervalStatePresent means the interval was expected — the market
	// was open — and a canonical Bar exists for it, provider-complete.
	IntervalStatePresent
	// IntervalStateClosed means the calendar reports the market was not
	// open for this interval (a weekend, a holiday, or a partial
	// closure). No Bar is expected, and none being present is not a gap.
	IntervalStateClosed
	// IntervalStateMissing means the calendar reports the market was
	// open, the interval has already elapsed, and no Bar is present: a
	// real gap.
	IntervalStateMissing
	// IntervalStateIncomplete means a Bar is present but the provider
	// itself declared the underlying record incomplete (OANDA's own
	// "complete" flag was false): data has arrived, but is not final.
	IntervalStateIncomplete
	// IntervalStateInProgress means the interval has not finished as of
	// the query time, so whether it will end up Present, Missing, or
	// Incomplete cannot yet be judged.
	IntervalStateInProgress
)

// String returns a human-readable IntervalState name.
func (s IntervalState) String() string {
	switch s {
	case IntervalStateUnknown:
		return "unknown"
	case IntervalStatePresent:
		return "present"
	case IntervalStateClosed:
		return "closed"
	case IntervalStateMissing:
		return "missing"
	case IntervalStateIncomplete:
		return "incomplete"
	case IntervalStateInProgress:
		return "in-progress"
	default:
		return fmt.Sprintf("IntervalState(%d)", uint8(s))
	}
}

// ErrNilCalendar is returned (wrapped) by ClassifyInterval when cal is
// nil.
var ErrNilCalendar = errors.New("marketdata: nil calendar")

// ClassifyInterval reports which IntervalState applies to the bar
// interval span, given cal's status at span's start, the current time
// now, whether a canonical Bar is present for span, and — when present —
// whether the provider declared its underlying record complete.
// providerComplete is ignored when present is false.
//
// now is supplied by the caller's own clock; ClassifyInterval never calls
// time.Now, so it stays deterministic in a backtest.
//
// Classification order is fixed and matters: a calendar closure always
// wins, regardless of how much time has passed or whether data happens
// to be present. An interval that has not yet elapsed is next,
// regardless of whether a still-forming Bar happens to already be
// present. Only once both are settled do presence and provider
// completeness decide Present, Missing, or Incomplete.
//
// ClassifyInterval samples the calendar at span.Start() only. A bar
// interval that straddles a calendar status boundary mid-span — possible
// for a UTC-aligned M1/H1/H4 bar very close to the weekly open or close,
// not for a session-aligned D1/W1 bar — is classified by its start;
// resolving that finer boundary is left to issue #79's coverage engine.
func ClassifyInterval(cal Calendar, span TimeRange, now time.Time, present, providerComplete bool) (IntervalState, error) {
	if cal == nil {
		return IntervalStateUnknown, fmt.Errorf("marketdata: classify interval: %w", ErrNilCalendar)
	}
	if cal.Status(span.Start()) != StatusOpen {
		return IntervalStateClosed, nil
	}
	if !span.Elapsed(now) {
		return IntervalStateInProgress, nil
	}
	if !present {
		return IntervalStateMissing, nil
	}
	if !providerComplete {
		return IntervalStateIncomplete, nil
	}
	return IntervalStatePresent, nil
}

// DatasetComplete formally defines dataset completeness (issue #74): a
// range is complete when every calendar-open interval in it is Present
// and every calendar-closed interval is accepted as legitimately absent.
// states holds the IntervalState of every expected interval boundary
// across a queried range; DatasetComplete reports true only when every
// entry is IntervalStatePresent or IntervalStateClosed. Any Missing,
// Incomplete, InProgress, or Unknown entry means the range is not (yet,
// or in Missing's case, ever) complete.
//
// DatasetComplete takes the already-classified states directly rather
// than a range and a data source: producing that slice for a real query
// range is issue #79's coverage engine, not this package. An empty
// states is vacuously complete.
func DatasetComplete(states []IntervalState) bool {
	for _, s := range states {
		if s != IntervalStatePresent && s != IntervalStateClosed {
			return false
		}
	}
	return true
}
