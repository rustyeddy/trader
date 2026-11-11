package sdk

import (
	"sync"
	"time"
)

// Clock exposes the host time observed by a guest strategy. Protocol v1 does
// not provide host-driven timers; timer capabilities require a future protocol.
type Clock interface {
	Now() time.Time
}

// hostClock is Environment.Clock's own concrete implementation: Now
// always returns whatever timestamp the host most recently supplied
// (SessionStart's own start_time, then each BarEvent's own bar time
// as it arrives) — never a real time.Now() call in this process (see
// the package doc comment's own "clock ownership" section). This
// gives a guest the identical clock-observation semantics an
// in-process strategy already has under both a real and a simulated
// host clock, without the guest ever owning a time source of its own.
type hostClock struct {
	mu  sync.Mutex
	now time.Time
}

var _ Clock = (*hostClock)(nil)

func newHostClock(start time.Time) *hostClock {
	return &hostClock{now: start}
}

// Now returns the most recently supplied host timestamp.
func (c *hostClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// set updates the timestamp Now returns — called each time the host
// supplies a new one (SessionStart, then every BarEvent).
func (c *hostClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}
