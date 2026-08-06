// Package clock provides Trader's deterministic time seam, as decided by
// ADR-015 (docs/arch/adr-015-deterministic-time-and-clock-abstraction.org)
// and issue #23 (M1-05).
//
// # Ownership
//
// clock is a composition-root support package. Domain and
// application-orchestration components that need the current time or timer
// behavior receive a Clock explicitly through their constructors; they
// never call time.Now, time.NewTimer, time.After, or time.Sleep directly.
// A pure function that only compares or transforms a caller-supplied
// timestamp does not need a Clock at all.
//
// This constraint targets domain and application code specifically, not
// every direct use of time in the module. Infrastructure adapters,
// composition roots, and test code (including benchmarks, which live in
// test files) are exempt. TestDomainCodeDoesNotCallTimeDirectly enforces
// this against an explicit list of exempt paths; a future package with a
// legitimate reason to use time directly is added to that list when it is
// introduced, rather than assumed exempt in advance.
//
// Production composition roots use Real. Tests, backtests, paper
// simulations, and the simulated broker use Simulated, which advances only
// when explicitly told to and never waits on wall-clock time.
//
// # Minimal contract
//
// The public contract is deliberately small: the current time, and only
// the one-shot timer behavior actually needed. There is no ticker,
// callback scheduler, or cron-like behavior, and this package does not
// reimplement the standard time package. Sleep and After convenience
// wrappers are deliberately absent too: a Sleep helper built over a
// manually advanced clock can deadlock the very goroutine expected to
// advance it, and the pattern below already covers the need without that
// risk.
//
// # Waiting on a timer
//
// Use NewTimer with select and context.Context cancellation rather than a
// blocking Sleep:
//
//	timer := c.NewTimer(timeout)
//	defer timer.Stop()
//
//	select {
//	case <-timer.C():
//	        return ErrTimeout
//	case <-ctx.Done():
//	        return ctx.Err()
//	}
//
// # Equal-deadline ordering
//
// Timers with equal deadlines are made ready in creation order. This
// guarantees the order in which independent timer channels are marked
// ready internally; it does not guarantee the order in which independent
// goroutines or a select statement observe or consume those channels. A
// globally observable execution order would require a scheduler or event
// queue rather than a channel-per-timer API, which this package
// deliberately does not provide.
//
// # UTC and monotonic metadata
//
// Both implementations return canonical UTC with Go's process-local
// monotonic-clock reading stripped, since the clock is itself the boundary
// where time enters Trader. Persisted or serialized timestamps must not
// depend on that monotonic reading, which is process-local and is not
// preserved by serialization in any case.
package clock
