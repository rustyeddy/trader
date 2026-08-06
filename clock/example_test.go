package clock_test

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/clock"
)

// ExampleReal shows the shape every consumer sees regardless of which
// implementation a composition root injects: a *slog-style dependency, not
// a global. Real{} needs no construction.
func ExampleReal() {
	var c clock.Clock = clock.Real{}

	fmt.Println(c.Now().Year() >= 2024)
	// Output:
	// true
}

// ExampleSimulated shows the deterministic-testing pattern this package
// exists for: a test advances time explicitly and observes a timer become
// ready, with no wall-clock wait anywhere in the test.
func ExampleSimulated() {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.NewSimulated(start)

	timer := c.NewTimer(5 * time.Second)

	if err := c.Advance(10 * time.Second); err != nil {
		fmt.Println("error:", err)
		return
	}

	select {
	case deadline := <-timer.C():
		fmt.Println(deadline.Sub(start))
	default:
		fmt.Println("timer did not fire")
	}
	// Output:
	// 5s
}

// ExampleTimer shows the recommended pattern for waiting on a timer with
// cancellation, in place of a blocking Sleep: NewTimer, Stop deferred so an
// early return still releases it, and a select against both the timer and
// context cancellation.
func ExampleTimer() {
	c := clock.NewSimulated(time.Now())

	run := func(ctx context.Context, timeout time.Duration) error {
		timer := c.NewTimer(timeout)
		defer timer.Stop()

		select {
		case <-timer.C():
			return fmt.Errorf("timed out")
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // simulate the operation being cancelled before it times out

	err := run(ctx, time.Minute)
	fmt.Println(err)
	// Output:
	// context canceled
}
