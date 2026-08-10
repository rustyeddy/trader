package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTimeRangeValid(t *testing.T) {
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	r, err := NewTimeRange(start, end)
	require.NoError(t, err)
	assert.Equal(t, start, r.Start())
	assert.Equal(t, end, r.End())
	assert.Equal(t, time.Hour, r.Duration())
}

func TestNewTimeRangeRejectsNonPositiveSpan(t *testing.T) {
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	_, err := NewTimeRange(start, start)
	assert.Error(t, err)

	_, err = NewTimeRange(start, start.Add(-time.Second))
	assert.Error(t, err)
}

func TestTimeRangeContainsIsHalfOpen(t *testing.T) {
	start := time.Date(2026, time.January, 1, 17, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	r, err := NewTimeRange(start, end)
	require.NoError(t, err)

	assert.True(t, r.Contains(start), "start is inclusive")
	assert.True(t, r.Contains(start.Add(30*time.Minute)))
	assert.False(t, r.Contains(end), "end is exclusive")
	assert.False(t, r.Contains(start.Add(-time.Nanosecond)))
}

func TestTimeRangeElapsed(t *testing.T) {
	start := time.Date(2026, time.January, 1, 17, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	r, err := NewTimeRange(start, end)
	require.NoError(t, err)

	assert.False(t, r.Elapsed(start), "not elapsed at start")
	assert.False(t, r.Elapsed(end.Add(-time.Nanosecond)), "not elapsed just before end")
	assert.True(t, r.Elapsed(end), "elapsed exactly at end")
	assert.True(t, r.Elapsed(end.Add(time.Hour)), "elapsed well after end")
}
