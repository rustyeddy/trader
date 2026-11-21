// Package clock is where Trader reads the time. Code that needs "now" calls
// Clock, or takes a func() time.Time and is handed Clock, instead of calling
// time.Now itself, so a test can pass a fixed time and, if Trader ever has a clock
// of its own (a simulated one for replay, say), this is the one place to change.
package clock

import "time"

// Clock returns the current time, in UTC with the monotonic reading stripped.
func Clock() time.Time {
	return time.Now().UTC().Round(0)
}
