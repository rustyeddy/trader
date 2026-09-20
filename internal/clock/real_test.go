package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealZeroValueIsUsable(t *testing.T) {
	var c Real
	assert.False(t, c.Now().IsZero())
}

func TestRealSatisfiesClock(t *testing.T) {
	var _ Clock = Real{}
}

func TestRealNowIsUTC(t *testing.T) {
	c := Real{}
	got := c.Now()
	assert.Equal(t, time.UTC, got.Location())
}

func TestRealNowHasNoMonotonicReading(t *testing.T) {
	c := Real{}
	got := c.Now()
	// time.Time.String appends " m=+<seconds>" when a monotonic reading is
	// present; its absence is the documented way to observe that Round(0)
	// actually stripped it, since the reading itself is otherwise
	// unexported and unobservable.
	assert.NotContains(t, got.String(), " m=")
}

func TestRealNowAdvances(t *testing.T) {
	c := Real{}
	first := c.Now()
	second := c.Now()
	assert.False(t, second.Before(first))
}

func TestRealTimerFires(t *testing.T) {
	c := Real{}
	timer := c.NewTimer(time.Millisecond)

	select {
	case <-timer.C():
	case <-time.After(time.Second):
		t.Fatal("timer did not fire within a generous bound")
	}
}

// TestRealTimerDeliversApproximateExpirationTime checks only what the
// shared Timer contract actually promises for Real: the delivered value
// identifies expiration, not necessarily the exact requested deadline
// (that stronger guarantee is Simulated-only — see
// TestSimulatedTimerDeliversScheduledDeadlineNotAdvancedTime).
func TestRealTimerDeliversApproximateExpirationTime(t *testing.T) {
	c := Real{}
	before := c.Now()
	timer := c.NewTimer(10 * time.Millisecond)

	select {
	case got := <-timer.C():
		assert.False(t, got.Before(before), "delivered time must not precede the timer's start")
	case <-time.After(time.Second):
		t.Fatal("timer did not fire within a generous bound")
	}
}

func TestRealTimerStopPreventsDelivery(t *testing.T) {
	c := Real{}
	timer := c.NewTimer(200 * time.Millisecond)

	ok := timer.Stop()
	assert.True(t, ok)

	select {
	case <-timer.C():
		t.Fatal("a stopped timer must not deliver")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRealTimerStopAfterFireReturnsFalse(t *testing.T) {
	c := Real{}
	timer := c.NewTimer(time.Millisecond)

	select {
	case <-timer.C():
	case <-time.After(time.Second):
		t.Fatal("timer did not fire within a generous bound")
	}

	assert.False(t, timer.Stop())
}

func TestRealTimerStopTwiceReturnsFalse(t *testing.T) {
	c := Real{}
	timer := c.NewTimer(time.Second)

	require.True(t, timer.Stop())
	assert.False(t, timer.Stop())
}

func TestRealTimerNonPositiveDurationFiresPromptly(t *testing.T) {
	c := Real{}
	timer := c.NewTimer(0)

	select {
	case <-timer.C():
	case <-time.After(time.Second):
		t.Fatal("a non-positive duration timer did not fire within a generous bound")
	}
}

func TestRealTimerChannelIsNotClosedAfterFiring(t *testing.T) {
	c := Real{}
	timer := c.NewTimer(0)

	<-timer.C()

	// A closed channel receive returns immediately with ok == false; an
	// empty, still-open channel does not, regardless of whether it happens
	// to be buffered — a real timer channel's capacity is a runtime detail
	// this package does not depend on. Use a non-blocking receive via
	// select+default to distinguish the two without hanging the test if the
	// (correct) behavior is "still open."
	select {
	case _, ok := <-timer.C():
		assert.True(t, ok, "the channel must not be closed after firing")
	default:
		// No second value pending, and the receive did not report closed:
		// consistent with a still-open, empty channel, which is correct.
	}
}
