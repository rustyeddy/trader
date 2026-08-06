package id

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomEntropyIsNonZeroAndVaries(t *testing.T) {
	var r Random

	a, err := r.Entropy()
	require.NoError(t, err)
	b, err := r.Entropy()
	require.NoError(t, err)

	assert.NotEqual(t, [10]byte{}, a, "crypto/rand producing all zero bytes is astronomically unlikely")
	assert.NotEqual(t, a, b, "two draws should not collide")
}

func TestGenerateProducesParsableID(t *testing.T) {
	c := clock.NewSimulated(time.Now())
	g := NewGenerator(c, NewDeterministic(1, 2))

	got, err := GenerateRunID(g)
	require.NoError(t, err)
	assert.False(t, got.IsZero())

	again, err := ParseRunID(got.String())
	require.NoError(t, err)
	assert.True(t, got.Equal(again))
}

func TestGenerateEmbedsClockTimestamp(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.NewSimulated(start)
	g := NewGenerator(c, NewDeterministic(1, 2))

	got, err := GenerateRunID(g)
	require.NoError(t, err)

	embedded, err := got.Time()
	require.NoError(t, err)
	assert.True(t, embedded.Equal(start))
}

func TestGenerateDifferentKindsProduceDifferentPrefixedStrings(t *testing.T) {
	c := clock.NewSimulated(time.Now())
	g := NewGenerator(c, NewDeterministic(1, 2))

	run, err := GenerateRunID(g)
	require.NoError(t, err)
	order, err := GenerateOrderID(g)
	require.NoError(t, err)

	assert.NotEqual(t, run.String()[:3], "")
	assert.Equal(t, "run", run.String()[:3])
	assert.Equal(t, "ord", order.String()[:3])
}

func TestGenerateSameMillisecondIsMonotonicallyIncreasing(t *testing.T) {
	// A Simulated clock that is never advanced makes every call within this
	// test land on the same millisecond, forcing the monotonic-entropy
	// path deterministically instead of hoping wall-clock timing happens to
	// line up.
	c := clock.NewSimulated(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g := NewGenerator(c, NewDeterministic(1, 2))

	var prev RunID
	for i := range 100 {
		got, err := GenerateRunID(g)
		require.NoError(t, err)

		if i > 0 {
			assert.Less(t, prev.String(), got.String(),
				"identifiers generated within the same millisecond must sort strictly increasing")
		}
		prev = got
	}
}

func TestGenerateAcrossAdvancingMillisecondsStaysOrdered(t *testing.T) {
	c := clock.NewSimulated(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g := NewGenerator(c, NewDeterministic(1, 2))

	var prev RunID
	for i := range 20 {
		if i > 0 {
			require.NoError(t, c.Advance(time.Millisecond))
		}
		got, err := GenerateRunID(g)
		require.NoError(t, err)

		if i > 0 {
			assert.Less(t, prev.String(), got.String())
		}
		prev = got
	}
}

func TestGenerateReportsEntropySourceError(t *testing.T) {
	c := clock.NewSimulated(time.Now())
	g := NewGenerator(c, failingSource{})

	_, err := GenerateRunID(g)
	require.Error(t, err)
}

func TestGenerateReportsEntropyExhaustion(t *testing.T) {
	c := clock.NewSimulated(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g := NewGenerator(c, maxEntropySource{})

	// First call draws fresh entropy (all 0xFF from this fixed source);
	// the second, still within the same millisecond, must increment past
	// the maximum representable value and report exhaustion rather than
	// silently wrapping around to zero and losing monotonicity.
	_, err := GenerateRunID(g)
	require.NoError(t, err)

	_, err = GenerateRunID(g)
	require.ErrorIs(t, err, ErrEntropyExhausted)
}

func TestGenerateReportsClockMovedBackward(t *testing.T) {
	g := &Generator{clock: &stepClock{times: []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 1_000_000, time.UTC), // .001s
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),         // .000s -- earlier
	}}, source: Random{}}

	_, err := GenerateRunID(g)
	require.NoError(t, err)

	_, err = GenerateRunID(g)
	require.ErrorIs(t, err, ErrClockMovedBackward)
}

func TestGeneratorConcurrentUseProducesUniqueIDs(t *testing.T) {
	c := clock.NewSimulated(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g := NewGenerator(c, NewDeterministic(1, 2))

	const n = 200
	results := make(chan RunID, n)
	errs := make(chan error, n)

	for range n {
		go func() {
			got, err := GenerateRunID(g)
			results <- got
			errs <- err
		}()
	}

	seen := make(map[string]bool, n)
	for range n {
		require.NoError(t, <-errs)
		got := <-results
		assert.False(t, seen[got.String()], "duplicate identifier generated under concurrent use")
		seen[got.String()] = true
	}
}

// TestDeterministicReproducesSequence is the concrete proof behind issue
// #24's "deterministic generation produces reproducible sequences"
// acceptance criterion: the same seed and the same simulated clock produce
// the exact same sequence of identifiers, every time.
func TestDeterministicReproducesSequence(t *testing.T) {
	run := func() []string {
		c := clock.NewSimulated(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		g := NewGenerator(c, NewDeterministic(42, 7))

		out := make([]string, 10)
		for i := range out {
			require.NoError(t, c.Advance(time.Millisecond))
			got, err := GenerateRunID(g)
			require.NoError(t, err)
			out[i] = got.String()
		}
		return out
	}

	first := run()
	second := run()
	assert.Equal(t, first, second)
}

func TestDeterministicDifferentSeedsDiffer(t *testing.T) {
	newSeq := func(seed uint64) string {
		c := clock.NewSimulated(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		g := NewGenerator(c, NewDeterministic(seed, seed))
		got, err := GenerateRunID(g)
		require.NoError(t, err)
		return got.String()
	}

	assert.NotEqual(t, newSeq(1), newSeq(2))
}

// failingSource always reports an error, for testing that Generate
// propagates a Source failure rather than falling back silently.
type failingSource struct{}

func (failingSource) Entropy() ([10]byte, error) {
	return [10]byte{}, assert.AnError
}

// maxEntropySource always returns the maximum representable entropy value,
// so the very next same-millisecond Generate call is guaranteed to
// overflow when incrementing it.
type maxEntropySource struct{}

func (maxEntropySource) Entropy() ([10]byte, error) {
	var b [10]byte
	for i := range b {
		b[i] = 0xFF
	}
	return b, nil
}

// stepClock is a clock.Clock returning each of times in order, one per
// Now() call, for testing exact sequences a Simulated clock cannot produce
// on its own (like time moving backward).
type stepClock struct {
	times []time.Time
	next  int
}

func (c *stepClock) Now() time.Time {
	t := c.times[c.next]
	c.next++
	return t
}

func (c *stepClock) NewTimer(d time.Duration) clock.Timer {
	panic("not implemented")
}
