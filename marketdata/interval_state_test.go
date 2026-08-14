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

	state, err := ClassifyInterval(c, span, now, true, true)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateClosed, state, "closed wins even though a bar is present")
}

func TestClassifyInterval_ClosedWinsOverPresence(t *testing.T) {
	c := NewFXCalendar(StandardFXHolidays(2029))
	span := h1Span(t, nyTime(2029, time.January, 1, 10, 0)) // full holiday, ordinary weekday
	now := span.End().Add(time.Hour)

	state, err := ClassifyInterval(c, span, now, true, true)
	require.NoError(t, err)
	assert.Equal(t, IntervalStateClosed, state)
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

func TestIntervalState_String(t *testing.T) {
	cases := map[IntervalState]string{
		IntervalStateUnknown:    "unknown",
		IntervalStatePresent:    "present",
		IntervalStateClosed:     "closed",
		IntervalStateMissing:    "missing",
		IntervalStateIncomplete: "incomplete",
		IntervalStateInProgress: "in-progress",
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
		{"one unknown", []IntervalState{IntervalStatePresent, IntervalStateUnknown}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DatasetComplete(tc.states))
		})
	}
}
