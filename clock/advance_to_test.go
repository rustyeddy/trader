package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimulatedAdvanceToMovesToExactTarget(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)
	target := mustParse(t, "2026-01-01T04:00:00Z")

	require.NoError(t, c.AdvanceTo(target))
	assert.True(t, c.Now().Equal(target), "Now() must be exactly the requested target")
}

// TestSimulatedAdvanceToEqualToNowIsANoOp mirrors
// TestSimulatedNowIsExactAfterAdvance's own Advance(0) case: a target
// equal to the current time is valid and leaves time unchanged.
func TestSimulatedAdvanceToEqualToNowIsANoOp(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)

	require.NoError(t, c.AdvanceTo(start))
	assert.True(t, c.Now().Equal(start))
}

// TestSimulatedAdvanceToEqualToNowStillFiresDueTimers proves the
// no-op case still fires any timer already due, matching Advance(0)'s
// own documented behavior.
func TestSimulatedAdvanceToEqualToNowStillFiresDueTimers(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)
	timer := c.NewTimer(0)

	require.NoError(t, c.AdvanceTo(start))
	select {
	case <-timer.C():
	default:
		t.Fatal("a timer already due must fire on a no-op AdvanceTo")
	}
}

func TestSimulatedAdvanceToRejectsTargetBeforeNow(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)
	require.NoError(t, c.AdvanceTo(start.Add(time.Hour)))

	err := c.AdvanceTo(start)
	require.ErrorIs(t, err, ErrNegativeAdvance)
	assert.True(t, c.Now().Equal(start.Add(time.Hour)), "a rejected AdvanceTo must not change the clock")
}

// TestSimulatedAdvanceToRejectsTargetBeforeNowLeavesTimersUnchanged
// proves a rejected AdvanceTo mutates neither time nor timer state: a
// timer that would have fired had the (rejected) advance actually
// happened must still be pending afterward.
func TestSimulatedAdvanceToRejectsTargetBeforeNowLeavesTimersUnchanged(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)
	timer := c.NewTimer(30 * time.Minute)

	err := c.AdvanceTo(start.Add(-time.Hour))
	require.ErrorIs(t, err, ErrNegativeAdvance)

	select {
	case <-timer.C():
		t.Fatal("a rejected AdvanceTo must not fire a timer")
	default:
	}
}

// TestSimulatedAdvanceToCanonicalizesTarget proves a target expressed
// in a different location, or carrying a monotonic reading, produces
// identical clock state to the equivalent UTC instant -- matching
// NewSimulated's own canonicalization (review feedback on #211).
func TestSimulatedAdvanceToCanonicalizesTarget(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")

	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	// 2026-01-01T04:00:00Z is 2025-12-31T20:00:00-08:00 in Los Angeles.
	targetInLA := time.Date(2025, 12, 31, 20, 0, 0, 0, loc)

	c := NewSimulated(start)
	require.NoError(t, c.AdvanceTo(targetInLA))

	assert.Equal(t, time.UTC, c.Now().Location())
	assert.True(t, c.Now().Equal(mustParse(t, "2026-01-01T04:00:00Z")))

	// A monotonic-carrying target (time.Now()) canonicalizes the same
	// way NewSimulated already does -- the resulting Now() carries no
	// monotonic reading.
	c2 := NewSimulated(start)
	require.NoError(t, c2.AdvanceTo(time.Now().Add(time.Hour)))
	assert.NotContains(t, c2.Now().String(), " m=")
}

// TestSimulatedAdvanceToFiresMultipleDeadlinesInOneCall mirrors
// TestSimulatedMultipleDeadlinesCrossedInOneAdvance for AdvanceTo:
// crossing several timers' deadlines in one call preserves the
// existing deadline/creation-order firing behavior.
func TestSimulatedAdvanceToFiresMultipleDeadlinesInOneCall(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)
	t1 := c.NewTimer(1 * time.Second)
	t2 := c.NewTimer(2 * time.Second)
	t3 := c.NewTimer(10 * time.Second)

	require.NoError(t, c.AdvanceTo(start.Add(5*time.Second)))

	for _, timer := range []Timer{t1, t2} {
		select {
		case <-timer.C():
		default:
			t.Fatal("a timer whose deadline was crossed must be ready")
		}
	}
	select {
	case <-t3.C():
		t.Fatal("a timer whose deadline was not crossed must not be ready")
	default:
	}

	assert.True(t, c.Now().Equal(start.Add(5*time.Second)), "Now() must equal the requested target exactly")
}

// TestSimulatedAdvanceToDeterministicAcrossIndependentInstances proves
// two independently constructed clocks given the same start, timers,
// and AdvanceTo sequence produce identical observations (review
// feedback on #211).
func TestSimulatedAdvanceToDeterministicAcrossIndependentInstances(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	targets := []time.Time{
		start.Add(1 * time.Hour),
		start.Add(90 * time.Minute),
		start.Add(4 * time.Hour),
	}

	run := func() (time.Time, []bool) {
		c := NewSimulated(start)
		timer := c.NewTimer(2 * time.Hour)
		var fired []bool
		for _, target := range targets {
			require.NoError(t, c.AdvanceTo(target))
			select {
			case <-timer.C():
				fired = append(fired, true)
			default:
				fired = append(fired, false)
			}
		}
		return c.Now(), fired
	}

	firstNow, firstFired := run()
	secondNow, secondFired := run()
	assert.True(t, firstNow.Equal(secondNow))
	assert.Equal(t, firstFired, secondFired)
}
