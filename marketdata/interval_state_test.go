package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// h1Span returns a one-hour TimeRange starting at start, for
// ClassifyInterval tests.
func h1Span(t *testing.T, start time.Time) TimeRange {
	t.Helper()
	span, err := NewTimeRange(start, start.Add(time.Hour))
	require.NoError(t, err)
	return span
}

func TestClassifyInterval_NilCalendar(t *testing.T) {
	span := h1Span(t, nyTime(2026, time.January, 6, 10, 0))
	state, err := ClassifyInterval(nil, span, span.End(), true, true)
	assert.ErrorIs(t, err, ErrNilCalendar)
	assert.Equal(t, IntervalStateUnknown, state)
}

func TestClassifyInterval_ClosedByCalendar(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 3, 10, 0)) // Saturday
	now := span.End().Add(time.Hour)

	state, err := ClassifyInterval(c, span, now, false, false)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateClosed, state)
}

func TestClassifyInterval_ClosedByFullHoliday(t *testing.T) {
	c := NewFXCalendar(StandardFXHolidays(2029))
	span := h1Span(t, nyTime(2029, time.January, 1, 10, 0)) // full holiday, ordinary weekday
	now := span.End().Add(time.Hour)

	state, err := ClassifyInterval(c, span, now, false, false)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateClosed, state)
}

// A present Bar during a calendar closure is a contradiction, not a
// routine closure: it must classify as Unexpected, never silently as
// Closed (which DatasetComplete treats as accepted-absent — collapsing
// this into Closed would hide a real calendar/data disagreement).
func TestClassifyInterval_PresentDuringClosureIsUnexpected(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 3, 10, 0)) // Saturday
	now := span.End().Add(time.Hour)

	state, err := ClassifyInterval(c, span, now, true, true)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateUnexpected, state)
}

func TestClassifyInterval_PresentDuringFullHolidayIsUnexpected(t *testing.T) {
	c := NewFXCalendar(StandardFXHolidays(2029))
	span := h1Span(t, nyTime(2029, time.January, 1, 10, 0))
	now := span.End().Add(time.Hour)

	state, err := ClassifyInterval(c, span, now, true, false)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateUnexpected, state, "providerComplete is irrelevant once the interval is Unexpected")
}

func TestClassifyInterval_InProgress(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 6, 10, 0)) // Tuesday, open
	now := span.Start().Add(30 * time.Minute)               // interval hasn't elapsed

	state, err := ClassifyInterval(c, span, now, false, false)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateInProgress, state)
}

func TestClassifyInterval_InProgressWinsOverPresence(t *testing.T) {
	// A still-forming Bar happening to already be present must not
	// promote the interval past InProgress.
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 6, 10, 0))
	now := span.Start().Add(30 * time.Minute)

	state, err := ClassifyInterval(c, span, now, true, true)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateInProgress, state)
}

func TestClassifyInterval_ElapsedBoundaryIsNotInProgress(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 6, 10, 0))
	now := span.End() // exactly elapsed

	state, err := ClassifyInterval(c, span, now, true, true)
	require.NoError(t, err)
	assert.Equal(t, IntervalStatePresent, state)
}

func TestClassifyInterval_Missing(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 6, 10, 0))
	now := span.End()

	state, err := ClassifyInterval(c, span, now, false, false)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateMissing, state)
}

func TestClassifyInterval_Incomplete(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 6, 10, 0))
	now := span.End()

	state, err := ClassifyInterval(c, span, now, true, false)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateIncomplete, state)
}

func TestClassifyInterval_Present(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 6, 10, 0))
	now := span.End()

	state, err := ClassifyInterval(c, span, now, true, true)
	require.NoError(t, err)
	assert.Equal(t, IntervalStatePresent, state)
}

func TestClassifyInterval_ProviderCompleteIgnoredWhenAbsent(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{})
	span := h1Span(t, nyTime(2026, time.January, 6, 10, 0))
	now := span.End()

	state, err := ClassifyInterval(c, span, now, false, true)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateMissing, state, "providerComplete is meaningless when present is false")
}

// --- Straddling a calendar boundary ---

func TestClassifyInterval_StraddlesPartialClosureThreshold(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{
		PartialClosures: []PartialClosure{
			{Date: nyTime(2026, time.December, 24, 0, 0), From: 13 * time.Hour},
		},
	})
	// 12:30-13:30 NY straddles the 13:00 partial-closure threshold.
	span, err := NewTimeRange(nyTime(2026, time.December, 24, 12, 30), nyTime(2026, time.December, 24, 13, 30))
	require.NoError(t, err)

	state, err := ClassifyInterval(c, span, span.End(), false, false)
	assert.ErrorIs(t, err, ErrIntervalStraddlesBoundary)
	assert.Equal(t, IntervalStateUnknown, state)
}

func TestClassifyInterval_StraddlesHolidaySundayReopen(t *testing.T) {
	holiday := nyTime(2023, time.January, 1, 0, 0) // a Sunday
	c := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{holiday}})
	// 23:30 Sunday (still closed) through 00:30 Monday (reopened)
	// straddles the New York midnight reopen.
	span, err := NewTimeRange(nyTime(2023, time.January, 1, 23, 30), nyTime(2023, time.January, 2, 0, 30))
	require.NoError(t, err)

	state, err := ClassifyInterval(c, span, span.End(), false, false)
	assert.ErrorIs(t, err, ErrIntervalStraddlesBoundary)
	assert.Equal(t, IntervalStateUnknown, state)
}

func TestClassifyInterval_FullyWithinTruncatedSessionOK(t *testing.T) {
	// A span fully inside the open portion of a truncated session must
	// still classify normally — straddle detection must not be
	// overzealous.
	c := NewFXCalendar(FXCalendarParams{
		PartialClosures: []PartialClosure{
			{Date: nyTime(2026, time.December, 24, 0, 0), From: 13 * time.Hour},
		},
	})
	span, err := NewTimeRange(nyTime(2026, time.December, 24, 9, 0), nyTime(2026, time.December, 24, 10, 0))
	require.NoError(t, err)

	state, classifyErr := ClassifyInterval(c, span, span.End(), true, true)
	require.NoError(t, classifyErr)
	assert.Equal(t, IntervalStatePresent, state)
}

func TestIntervalState_String(t *testing.T) {
	cases := map[IntervalState]string{
		IntervalStateUnknown:    "unknown",
		IntervalStatePresent:    "present",
		IntervalStateClosed:     "closed",
		IntervalStateMissing:    "missing",
		IntervalStateIncomplete: "incomplete",
		IntervalStateInProgress: "in-progress",
		IntervalStateUnexpected: "unexpected",
		IntervalState(200):      "IntervalState(200)",
	}
	for state, want := range cases {
		assert.Equal(t, want, state.String())
	}
}

func TestDatasetComplete(t *testing.T) {
	tests := []struct {
		name   string
		states []IntervalState
		want   bool
	}{
		{"empty", nil, true},
		{"all present", []IntervalState{IntervalStatePresent, IntervalStatePresent}, true},
		{"all closed", []IntervalState{IntervalStateClosed, IntervalStateClosed}, true},
		{"present and closed mixed", []IntervalState{IntervalStatePresent, IntervalStateClosed, IntervalStatePresent}, true},
		{"one missing", []IntervalState{IntervalStatePresent, IntervalStateMissing}, false},
		{"one incomplete", []IntervalState{IntervalStatePresent, IntervalStateIncomplete}, false},
		{"one in progress", []IntervalState{IntervalStatePresent, IntervalStateInProgress}, false},
		{"one unexpected", []IntervalState{IntervalStatePresent, IntervalStateUnexpected}, false},
		{"one unknown", []IntervalState{IntervalStatePresent, IntervalStateUnknown}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DatasetComplete(tc.states))
		})
	}
}

// A span can start and end open (matching Status samples at both ends)
// while still skipping over a closed block in the middle — a giant span
// that opens before a partial-closure threshold and extends into the
// next, unaffected session. uniformStatus's Session-containment check
// exists specifically to catch this even when the start/end Status
// samples alone would agree.
func TestClassifyInterval_StraddlesMidSpanClosureDespiteMatchingEndpoints(t *testing.T) {
	c := NewFXCalendar(FXCalendarParams{
		PartialClosures: []PartialClosure{
			// An artificially early threshold so most of Dec 24's
			// session is closed, isolating the open sliver at its start.
			{Date: nyTime(2026, time.December, 24, 0, 0), From: time.Hour},
		},
	})
	// Open at 00:30 Dec 24 (before the 01:00 threshold); open again at
	// 00:30 Dec 25 (the next, unaffected session) — but the entire
	// 01:00-17:00 Dec 24 block in between is closed.
	span, err := NewTimeRange(nyTime(2026, time.December, 24, 0, 30), nyTime(2026, time.December, 25, 0, 30))
	require.NoError(t, err)
	require.Equal(t, StatusOpen, c.Status(span.Start()))
	require.Equal(t, StatusOpen, c.Status(span.End().Add(-time.Nanosecond)))

	state, err := ClassifyInterval(c, span, span.End(), false, false)
	assert.ErrorIs(t, err, ErrIntervalStraddlesBoundary)
	assert.Equal(t, IntervalStateUnknown, state)
}
