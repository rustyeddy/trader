package clock

import (
	"sync"
	"testing"
	"time"
)

// TestSimulatedConcurrentAdvanceStopAndNewTimer exercises Advance, NewTimer,
// and Stop interleaving across many goroutines. Its point is what
// `go test -race` proves rather than any particular assertion on final
// state: since the goroutines race against each other, which timers end up
// fired versus stopped is inherently nondeterministic. What must hold
// regardless of interleaving is the absence of a data race, a deadlock, and
// a panic — Simulated's single mutex is meant to make every one of these
// operations safe to interleave arbitrarily.
func TestSimulatedConcurrentAdvanceStopAndNewTimer(t *testing.T) {
	c := NewSimulated(time.Now())

	var wg sync.WaitGroup
	for range 200 {
		wg.Go(func() {
			timer := c.NewTimer(time.Millisecond)
			timer.Stop()
		})
	}
	for range 50 {
		wg.Go(func() {
			_ = c.Advance(time.Millisecond)
		})
	}
	for range 50 {
		wg.Go(func() {
			_ = c.Now()
		})
	}
	wg.Wait()
}
