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
	// closure), and no Bar is present. No Bar is expected, and none
	// being present is not a gap.
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
	// IntervalStateUnexpected means the calendar reports the market was
	// not open for this interval, yet a Bar is present anyway. This is a
	// contradiction, not a routine closure: either the calendar (holiday
	// list, DST rule) is wrong, the data is misaligned, or the data is
	// simply wrong. It must never be treated the same as
	// IntervalStateClosed — see DatasetComplete, which rejects it.
	IntervalStateUnexpected
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
	case IntervalStateUnexpected:
		return "unexpected"
	default:
		return fmt.Sprintf("IntervalState(%d)", uint8(s))
	}
}

// Sentinel errors returned (wrapped) by ClassifyInterval.
var (
	// ErrNilCalendar marks a nil Calendar argument.
	ErrNilCalendar = errors.New("marketdata: nil calendar")
	// ErrIntervalStraddlesBoundary marks a span ClassifyInterval cannot
	// safely classify as a whole: the calendar reports more than one
	// Status somewhere within span, so no single IntervalState can
	// honestly describe it. Splitting or otherwise resolving such a span
	// is the caller's decision, not something ClassifyInterval guesses
	// at by sampling only its start.
	ErrIntervalStraddlesBoundary = errors.New("marketdata: interval straddles a calendar status boundary")
)

// ClassifyInterval reports which IntervalState applies to the bar
// interval span, given cal's status throughout span, the current time
// now, whether a canonical Bar is present for span, and — when present —
// whether the provider declared its underlying record complete.
// providerComplete is ignored when present is false.
//
// now is supplied by the caller's own clock; ClassifyInterval never calls
// time.Now, so it stays deterministic in a backtest.
//
// Classification order is fixed and matters. First, span must be
// uniformly one Status throughout — see below. Given that: a calendar
// closure wins over presence, except that a present Bar during a closure
// is IntervalStateUnexpected, not IntervalStateClosed, because a bar
// existing when the calendar says the market was shut is a contradiction
// that must never silently read as a routine, accepted closure (see
// DatasetComplete). An interval that has not yet elapsed comes next,
// regardless of whether a still-forming Bar happens to already be
// present. Only once both are settled do presence and provider
// completeness decide Present, Missing, or Incomplete.
//
// # Straddling a calendar boundary
//
// A bar interval spanning a status change — for example a UTC-aligned H4
// bar that starts before a 13:00 New York partial-closure threshold and
// ends after it — cannot be honestly described by one IntervalState.
// ClassifyInterval detects this by requiring the calendar to report the
// same Status at span's start and just before span's end, and, when that
// Status is StatusOpen, requiring cal.Session(span.Start()) to fully
// contain span. If either check fails, ClassifyInterval returns a
// wrapped ErrIntervalStraddlesBoundary instead of guessing from span's
// start alone; splitting or otherwise resolving such a span is left to
// the caller (issue #79's coverage engine, in practice). This cannot
// happen for a session-aligned D1/W1 bar, whose span already equals one
// Calendar-defined session or week.
func ClassifyInterval(cal Calendar, span TimeRange, now time.Time, present, providerComplete bool) (IntervalState, error) {
	if cal == nil {
		return IntervalStateUnknown, fmt.Errorf("marketdata: classify interval: %w", ErrNilCalendar)
	}
	status, err := uniformStatus(cal, span)
	if err != nil {
		return IntervalStateUnknown, err
	}
	if status != StatusOpen {
		if present {
			return IntervalStateUnexpected, nil
		}
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

// uniformStatus returns cal's Status throughout span, and an error
// wrapping ErrIntervalStraddlesBoundary if span is not uniformly one
// Status. See ClassifyInterval's "Straddling a calendar boundary"
// section for exactly what is checked and why.
func uniformStatus(cal Calendar, span TimeRange) (Status, error) {
	start := cal.Status(span.Start())
	end := cal.Status(span.End().Add(-time.Nanosecond))
	if start != end {
		return StatusUnknown, fmt.Errorf("marketdata: classify interval: [%s, %s): %w", span.Start(), span.End(), ErrIntervalStraddlesBoundary)
	}
	if start == StatusOpen {
		session, ok := cal.Session(span.Start())
		if !ok || session.Start().After(span.Start()) || session.End().Before(span.End()) {
			return StatusUnknown, fmt.Errorf("marketdata: classify interval: [%s, %s): %w", span.Start(), span.End(), ErrIntervalStraddlesBoundary)
		}
	}
	return start, nil
}

// DatasetComplete formally defines dataset completeness (issue #74): a
// range is complete when every calendar-open interval in it is Present
// and every calendar-closed interval is accepted as legitimately absent.
// states holds the IntervalState of every expected interval boundary
// across a queried range; DatasetComplete reports true only when every
// entry is IntervalStatePresent or IntervalStateClosed. Any Missing,
// Incomplete, InProgress, Unexpected, or Unknown entry means the range is
// not (yet, or in Missing's and Unexpected's cases, ever, without
// correction) complete — in particular, IntervalStateUnexpected is never
// treated as equivalent to IntervalStateClosed, so a bar contradicting
// the calendar can never make a dataset look complete.
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
