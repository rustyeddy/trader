package id

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func fixed(at time.Time) func() time.Time { return func() time.Time { return at } }

func TestRandom_Entropy(t *testing.T) {
	var r Random
	a, err := r.Entropy()
	require.NoError(t, err)
	b, err := r.Entropy()
	require.NoError(t, err)

	assert.NotEqual(t, [10]byte{}, a, "crypto/rand giving all zero bytes is astronomically unlikely")
	assert.NotEqual(t, a, b, "two draws should not collide")
}

func TestGenerator_RunID(t *testing.T) {
	g := NewGenerator(fixed(t0), NewDeterministic(1, 2))
	got, err := g.RunID()
	require.NoError(t, err)

	assert.False(t, got.IsZero())
	assert.True(t, strings.HasPrefix(got.String(), "run_"))
	again, err := ParseRunID(got.String())
	require.NoError(t, err)
	assert.True(t, got.Equal(again), "what it makes, ParseRunID accepts")
}

func TestGenerator_SameMillisecondIsMonotonic(t *testing.T) {
	// A clock that never moves lands every call on one millisecond, which forces the
	// monotonic-entropy path.
	g := NewGenerator(fixed(t0), NewDeterministic(1, 2))

	var prev RunID
	for i := range 100 {
		got, err := g.RunID()
		require.NoError(t, err)
		if i > 0 {
			assert.Less(t, prev.String(), got.String(), "strictly increasing within one millisecond")
		}
		prev = got
	}
}

func TestGenerator_AdvancingMillisecondsStaysOrdered(t *testing.T) {
	now := t0
	g := NewGenerator(func() time.Time { return now }, NewDeterministic(1, 2))

	var prev RunID
	for i := range 20 {
		now = t0.Add(time.Duration(i) * time.Millisecond)
		got, err := g.RunID()
		require.NoError(t, err)
		if i > 0 {
			assert.Less(t, prev.String(), got.String())
		}
		prev = got
	}
}

func TestGenerator_ReportsEntropyError(t *testing.T) {
	_, err := NewGenerator(fixed(t0), failingSource{}).RunID()
	require.ErrorIs(t, err, assert.AnError)
}

func TestGenerator_ReportsEntropyExhaustion(t *testing.T) {
	g := NewGenerator(fixed(t0), maxEntropySource{})

	// The first call draws all 0xFF; the second, in the same millisecond, cannot
	// increment past it and must say so rather than wrap to zero.
	_, err := g.RunID()
	require.NoError(t, err)
	_, err = g.RunID()
	require.ErrorIs(t, err, ErrEntropyExhausted)
}

func TestGenerator_ClockMovingBackwardContinuesFromTheLastMillisecond(t *testing.T) {
	// 5 ms, then back to 2 ms, then forward again to 9 ms.
	times := []time.Time{t0.Add(5 * time.Millisecond), t0.Add(2 * time.Millisecond), t0.Add(9 * time.Millisecond)}
	g := NewGenerator(func() time.Time { t := times[0]; times = times[1:]; return t }, NewDeterministic(1, 2))

	var ids []RunID
	for range 3 {
		got, err := g.RunID()
		require.NoError(t, err, "a run can always get an ID")
		ids = append(ids, got)
	}

	assert.Less(t, ids[0].String(), ids[1].String(), "still strictly increasing after the step back")
	assert.Less(t, ids[1].String(), ids[2].String())
	second, err := ids[1].Time()
	require.NoError(t, err)
	assert.True(t, second.Equal(t0.Add(5*time.Millisecond)), "stamped with the last millisecond used, not the earlier one: %s", second)
	third, err := ids[2].Time()
	require.NoError(t, err)
	assert.True(t, third.Equal(t0.Add(9*time.Millisecond)), "and follows the clock again once it passes it")
}

func TestGenerator_ConcurrentUseGivesUniqueIDs(t *testing.T) {
	g := NewGenerator(fixed(t0), NewDeterministic(1, 2))

	const n = 200
	results := make(chan RunID, n)
	errs := make(chan error, n)
	for range n {
		go func() {
			got, err := g.RunID()
			results <- got
			errs <- err
		}()
	}

	seen := make(map[string]bool, n)
	for range n {
		require.NoError(t, <-errs)
		got := (<-results).String()
		assert.False(t, seen[got], "duplicate ID under concurrent use")
		seen[got] = true
	}
}

// failingSource always errors, to show a Generator reports it instead of falling back.
type failingSource struct{}

func (failingSource) Entropy() ([10]byte, error) { return [10]byte{}, assert.AnError }

// maxEntropySource always returns the largest entropy, so the next call in the same
// millisecond must overflow when it increments it.
type maxEntropySource struct{}

func (maxEntropySource) Entropy() ([10]byte, error) {
	var b [10]byte
	for i := range b {
		b[i] = 0xFF
	}
	return b, nil
}
