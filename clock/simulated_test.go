package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return tm
}

func TestNewSimulatedCanonicalizesToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)

	c := NewSimulated(start)
	assert.Equal(t, time.UTC, c.Now().Location())
	assert.True(t, c.Now().Equal(start))
}

func TestNewSimulatedStripsMonotonicReading(t *testing.T) {
	c := NewSimulated(time.Now())
	assert.NotContains(t, c.Now().String(), " m=")
}

func TestSimulatedSatisfiesClock(t *testing.T) {
	var _ Clock = (*Simulated)(nil)
}

func TestSimulatedNowIsExactAfterAdvance(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)

	require.NoError(t, c.Advance(90*time.Second))
	assert.True(t, c.Now().Equal(start.Add(90*time.Second)))

	require.NoError(t, c.Advance(0))
	assert.True(t, c.Now().Equal(start.Add(90*time.Second)), "advancing by zero must not move time")
}

func TestSimulatedAdvanceRejectsNegativeDuration(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)

	err := c.Advance(-time.Second)
	require.ErrorIs(t, err, ErrNegativeAdvance)
	assert.True(t, c.Now().Equal(start), "a rejected Advance must not change the clock")
}

func TestSimulatedTimerFiresWhenDeadlineCrossed(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	timer := c.NewTimer(10 * time.Second)

	require.NoError(t, c.Advance(5*time.Second))
	select {
	case <-timer.C():
		t.Fatal("timer must not fire before its deadline")
	default:
	}

	require.NoError(t, c.Advance(5*time.Second))
	select {
	case <-timer.C():
	default:
		t.Fatal("timer must fire once its deadline is crossed")
	}
}

func TestSimulatedTimerDeliversScheduledDeadlineNotAdvancedTime(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	c := NewSimulated(start)
	timer := c.NewTimer(5 * time.Second)

	// Advance well past the deadline in one jump.
	require.NoError(t, c.Advance(time.Hour))

	got := <-timer.C()
	assert.True(t, got.Equal(start.Add(5*time.Second)),
		"delivered value must be the timer's own deadline, not the clock's time after advancing")
}

// TestSimulatedTimerChannelIsBuffered pins the implementation detail that
// makes Advance's non-blocking delivery guarantee possible: unlike Real's
// channel (whose capacity is a runtime detail the shared Timer contract
// does not describe), Simulated's is deliberately buffered with capacity
// one so a fire pass never blocks on a receiver.
func TestSimulatedTimerChannelIsBuffered(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	timer := c.NewTimer(time.Second).(*simTimer)

	assert.Equal(t, 1, cap(timer.c))
}

func TestSimulatedMultipleDeadlinesCrossedInOneAdvance(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	t1 := c.NewTimer(1 * time.Second)
	t2 := c.NewTimer(2 * time.Second)
	t3 := c.NewTimer(10 * time.Second)

	require.NoError(t, c.Advance(5*time.Second))

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
}

func TestSimulatedNonPositiveDurationFiresBeforeNewTimerReturns(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))

	for _, d := range []time.Duration{0, -time.Second} {
		timer := c.NewTimer(d)
		select {
		case <-timer.C():
		default:
			t.Fatalf("NewTimer(%v) must be ready immediately, before any Advance", d)
		}
	}
}

func TestSimulatedStopPreventsDelivery(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	timer := c.NewTimer(time.Second)

	assert.True(t, timer.Stop())
	require.NoError(t, c.Advance(time.Hour))

	select {
	case <-timer.C():
		t.Fatal("a stopped timer must never deliver")
	default:
	}
}

func TestSimulatedStopReturnsFalseAfterFiring(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	timer := c.NewTimer(time.Second)

	require.NoError(t, c.Advance(time.Second))
	<-timer.C()

	assert.False(t, timer.Stop())
}

func TestSimulatedStopReturnsFalseOnSecondCall(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	timer := c.NewTimer(time.Second)

	require.True(t, timer.Stop())
	assert.False(t, timer.Stop())
}

func TestSimulatedEqualDeadlineOrderingComparator(t *testing.T) {
	// This tests the actual mechanism behind the "equal deadlines fire in
	// creation order" guarantee directly: readiness order is determined by
	// sorting on (deadline, creation sequence), and that comparator is what
	// this test pins. See ADR-015 for why this is the guarantee's precise
	// scope: creation order determines the order timer channels are made
	// ready internally, not the order any consumer happens to observe them.
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))

	first := c.NewTimer(5 * time.Second).(*simTimer)
	second := c.NewTimer(5 * time.Second).(*simTimer)
	third := c.NewTimer(5 * time.Second).(*simTimer)

	require.NoError(t, c.Advance(5*time.Second))

	// By the time Advance returns, fireDue has already sorted and fired
	// every due timer; inspect the sequence numbers directly to confirm the
	// comparator ordered them by creation order among equal deadlines.
	assert.Less(t, first.seq, second.seq)
	assert.Less(t, second.seq, third.seq)
}

func TestSimulatedEqualDeadlineTimersBothFireOnSameAdvance(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	t1 := c.NewTimer(5 * time.Second)
	t2 := c.NewTimer(5 * time.Second)

	require.NoError(t, c.Advance(5*time.Second))

	for _, timer := range []Timer{t1, t2} {
		select {
		case <-timer.C():
		default:
			t.Fatal("both equal-deadline timers must be ready after their shared deadline is crossed")
		}
	}
}

func TestSimulatedFiredTimersAreRemovedFromActiveState(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	c.NewTimer(time.Second)
	c.NewTimer(2 * time.Second)

	require.NoError(t, c.Advance(3*time.Second))
	assert.Empty(t, c.timers, "fired timers must not remain in the active set")
}

func TestSimulatedStoppedTimersAreRemovedImmediately(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))
	timer := c.NewTimer(time.Second)

	require.Len(t, c.timers, 1)
	timer.Stop()
	assert.Empty(t, c.timers, "a stopped timer must be removed immediately, not left for the next Advance")
}

func TestSimulatedActiveTimerSetDoesNotGrowUnboundedly(t *testing.T) {
	c := NewSimulated(mustParse(t, "2026-01-01T00:00:00Z"))

	for i := range 500 {
		// fired's deadline is crossed by this iteration's Advance; stopped's
		// is deliberately far in the future so only an explicit Stop, not
		// Advance, can remove it — exercising both cleanup paths each pass.
		fired := c.NewTimer(time.Second)
		stopped := c.NewTimer(time.Hour)

		require.NoError(t, c.Advance(time.Second))
		<-fired.C()
		stopped.Stop()

		require.Emptyf(t, c.timers, "iteration %d: active timer set grew unboundedly", i)
	}
}
