package strategysdk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHostClock_NowReflectsMostRecentSet(t *testing.T) {
	start := time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)
	c := newHostClock(start)
	require.True(t, c.Now().Equal(start))

	next := start.Add(time.Hour)
	c.set(next)
	require.True(t, c.Now().Equal(next))
}

func TestHostClock_NewTimerDelegatesToRealTimer(t *testing.T) {
	c := newHostClock(time.Now())
	timer := c.NewTimer(10 * time.Millisecond)
	defer timer.Stop()

	select {
	case <-timer.C():
	case <-time.After(2 * time.Second):
		t.Fatal("timer never fired")
	}
}
