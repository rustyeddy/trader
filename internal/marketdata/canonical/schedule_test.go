package canonical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ny(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	return l
}

func TestAligned(t *testing.T) {
	l := ny(t)
	for name, tc := range map[string]struct {
		i    Interval
		at   time.Time
		want bool
	}{
		"h1 on the hour":    {H1, utc(2024, 1, 2, 5), true},
		"h1 minute off":     {H1, utc(2024, 1, 2, 5).Add(time.Minute), false},
		"h1 nanosecond off": {H1, utc(2024, 1, 2, 5).Add(time.Nanosecond), false},
		"d1 winter 22:00Z":  {D1, utc(2024, 1, 1, 22), true},
		"d1 winter 21:00Z":  {D1, utc(2024, 1, 1, 21), false},
		"d1 summer 21:00Z":  {D1, utc(2024, 7, 1, 21), true},
		"d1 summer 22:00Z":  {D1, utc(2024, 7, 1, 22), false},
		"d1 as local 17:00": {D1, time.Date(2024, 3, 12, 17, 0, 0, 0, l), true},
		"d1 midnight UTC":   {D1, utc(2024, 1, 2, 0), false},
		"d1 17:00:01":       {D1, time.Date(2024, 3, 12, 17, 0, 1, 0, l), false},
		"d1 17:00 + 1ns":    {D1, time.Date(2024, 3, 12, 17, 0, 0, 1, l), false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Aligned(tc.i, tc.at)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	_, err := Aligned("h4", utc(2024, 1, 1, 0))
	assert.ErrorIs(t, err, ErrUnsupportedInterval)
}

func TestAvailableAt(t *testing.T) {
	for name, tc := range map[string]struct {
		i          Interval
		open, want time.Time
	}{
		"h1":                      {H1, utc(2024, 1, 2, 5), utc(2024, 1, 2, 6)},
		"h1 across midnight":      {H1, utc(2024, 1, 2, 23), utc(2024, 1, 3, 0)},
		"d1 winter is 24 hours":   {D1, utc(2024, 1, 2, 22), utc(2024, 1, 3, 22)},
		"d1 summer is 24 hours":   {D1, utc(2024, 7, 2, 21), utc(2024, 7, 3, 21)},
		"d1 spring forward, 23 h": {D1, utc(2024, 3, 9, 22), utc(2024, 3, 10, 21)},  // Sat 17:00 EST to Sun 17:00 EDT
		"d1 fall back, 25 h":      {D1, utc(2024, 11, 2, 21), utc(2024, 11, 3, 22)}, // Sat 17:00 EDT to Sun 17:00 EST
		"d1 month end":            {D1, utc(2024, 1, 31, 22), utc(2024, 2, 1, 22)},
		"d1 year end":             {D1, utc(2023, 12, 31, 22), utc(2024, 1, 1, 22)},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := AvailableAt(tc.i, tc.open)

			require.NoError(t, err)
			assert.True(t, got.Equal(tc.want), "got %s want %s", got, tc.want)
			assert.Equal(t, time.UTC, got.Location())
		})
	}
}

func TestAvailableAt_Refusals(t *testing.T) {
	_, err := AvailableAt(H1, utc(2024, 1, 2, 5).Add(time.Second))
	assert.ErrorIs(t, err, ErrMisaligned)
	_, err = AvailableAt(D1, utc(2024, 1, 2, 0))
	assert.ErrorIs(t, err, ErrMisaligned)
	_, err = AvailableAt("m1", utc(2024, 1, 2, 0))
	assert.ErrorIs(t, err, ErrUnsupportedInterval)
}

func TestInterval_Valid(t *testing.T) {
	assert.True(t, H1.Valid())
	assert.True(t, D1.Valid())
	assert.False(t, Interval("h4").Valid())
	assert.False(t, Interval("").Valid())
}
