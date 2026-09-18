package strategysdk

import (
	"sync"
	"time"

	"github.com/rustyeddy/trader/clock"
)

// hostClock is Environment.Clock's own concrete implementation: Now
// always returns whatever timestamp the host most recently supplied
// (SessionStart's own start_time, then each BarEvent's own bar time
// as it arrives) — never a real time.Now() call in this process (see
// the package doc comment's own "clock ownership" section). This
// gives a guest the identical clock-observation semantics an
// in-process strategy already has under both a real and a simulated
// host clock, without the guest ever owning a time source of its own.
//
// NewTimer is the one exception: it delegates to clock.Real, real
// wall-clock timers, via embedding. v1 has no wire mechanism for the
// host to drive a guest-local timer's own firing, and a guest process
// is, regardless of the host's own simulated-or-real clock, always a
// genuine external process running in real wall-clock time — any
// component that needs to actually wait must eventually rely on a
// real OS timer somewhere. This is a deliberate, narrow limitation
// documented here, not an oversight: a strategy that never calls
// env.Clock.NewTimer (as neither strategy/smatrend nor
// strategy/emacross does today) never observes it.
type hostClock struct {
	clock.Real

	mu  sync.Mutex
	now time.Time
}

var _ clock.Clock = (*hostClock)(nil)

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
