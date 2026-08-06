package clock

import "time"

// Clock is the minimal seam code depends on instead of calling time.Now or
// time.NewTimer directly. See the package doc comment for the ownership
// rule and the full set of guarantees each implementation provides.
type Clock interface {
	// Now returns the current time, in UTC with any monotonic-clock reading
	// stripped.
	Now() time.Time

	// NewTimer starts a new one-shot Timer that will become ready after d.
	// A non-positive d is ready before NewTimer returns.
	NewTimer(d time.Duration) Timer
}

// Timer is a one-shot timer, matching the shape of time.Timer: a
// never-closed channel that reports expiration, and a Stop method that
// prevents delivery if called before then.
//
// This shared contract deliberately promises less than either concrete
// implementation actually delivers: Real's channel comes directly from
// time.Timer, whose documented behavior is "the current time will be sent
// on C" at expiration — not necessarily the exact instant requested, and,
// since Go 1.23, not literally a buffered channel (its capacity is an
// unexported runtime implementation detail, not part of time.Timer's own
// contract). Simulated provides both stronger guarantees — exact scheduled-
// deadline delivery and an internally buffered channel — documented on
// Simulated itself rather than promised here, where they would not hold for
// Real.
type Timer interface {
	// C returns the channel on which the timer reports that it has
	// expired. The delivered value identifies the expiration instant; it is
	// never closed.
	C() <-chan time.Time

	// Stop prevents the timer from firing, if it has not already. It
	// returns true only on the transition from pending to stopped: false
	// if the timer already fired, and false if a prior call already
	// stopped it. Once Stop returns true, no value will ever be delivered
	// on C.
	Stop() bool
}
