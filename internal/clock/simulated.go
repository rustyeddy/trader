package clock

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNegativeAdvance reports an attempt to move a Simulated clock backward.
// Simulated time never moves backward.
var ErrNegativeAdvance = errors.New("clock: Advance does not accept a negative duration")

// Simulated is a manually advanced Clock for deterministic tests,
// backtests, and paper simulations. It advances only when Advance is
// called and never waits on wall-clock time.
//
// Simulated must be constructed with NewSimulated; its zero value is not
// usable — see the package doc comment for why. It contains no goroutines
// of its own: Now, NewTimer, Advance, and Stop are all synchronous, and
// timer delivery is a non-blocking send on a buffered channel. It is safe
// for concurrent use.
//
// Simulated's timers provide two guarantees beyond the shared Timer
// contract, neither of which Real promises: delivery carries the exact
// scheduled deadline, even when a single Advance call crosses several
// timers' deadlines by a larger amount than any one of them needed, and the
// channel is internally buffered with capacity one, which is what lets
// Advance deliver without blocking on a receiver. The buffering is an
// implementation detail that makes the non-blocking delivery guarantee
// possible; callers should rely on that guarantee, not on the channel's
// capacity itself.
type Simulated struct {
	mu     sync.Mutex
	now    time.Time
	timers []*simTimer
	seq    uint64
}

// NewSimulated returns a Simulated clock whose current time is start,
// canonicalized to UTC with any monotonic-clock reading stripped.
func NewSimulated(start time.Time) *Simulated {
	return &Simulated{now: start.UTC().Round(0)}
}

var _ Clock = (*Simulated)(nil)

// Now returns the clock's current simulated time.
func (s *Simulated) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

// NewTimer starts a new simulated Timer with a deadline of
// s.Now().Add(d). A non-positive d is ready before NewTimer returns,
// matching time.Timer; such a timer is never added to the clock's active
// set, since it has nothing left to wait for.
func (s *Simulated) NewTimer(d time.Duration) Timer {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	t := &simTimer{
		clock:    s,
		deadline: s.now.Add(d),
		seq:      s.seq,
		c:        make(chan time.Time, 1),
	}

	if d <= 0 {
		t.fired = true
		t.c <- t.deadline
		return t
	}

	s.timers = append(s.timers, t)
	return t
}

// Advance moves the clock's current time forward by d, firing every timer
// whose deadline is now due. Advance never waits on wall-clock time and
// returns ErrNegativeAdvance without changing the clock if d is negative.
func (s *Simulated) Advance(d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.advanceBy(d)
}

// AdvanceTo moves the clock's current time forward to exactly t, firing
// every timer due at or before t — the primitive a historical-timestamp-
// driven backtest scheduler needs (issue #211, M5-03), so it never has to
// compute t.Sub(Now()) itself at every call site. t is canonicalized to
// UTC with any monotonic reading stripped (matching NewSimulated's own
// canonicalization) before comparison, so equivalent instants expressed
// in different locations or carrying monotonic metadata never produce
// different clock state. AdvanceTo rejects a t before the clock's
// current time with ErrNegativeAdvance, leaving both time and timer
// state unchanged, the same way Advance itself rejects a negative
// duration; t equal to the current time is a valid no-op. AdvanceTo takes
// no context.Context: like Advance, it is a synchronous, bounded state
// mutation with no blocking work, and cancellation policy for a
// potentially long-running replay loop belongs to the caller driving
// that loop (the scheduler, #213), not to this primitive.
func (s *Simulated) AdvanceTo(t time.Time) error {
	target := t.UTC().Round(0)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.advanceBy(target.Sub(s.now))
}

// advanceBy must be called with s.mu held. It applies duration d,
// rejecting a negative one without changing state — the shared core
// both Advance and AdvanceTo delegate to, so the two can never
// disagree about what "advance" means.
func (s *Simulated) advanceBy(d time.Duration) error {
	if d < 0 {
		return ErrNegativeAdvance
	}
	s.now = s.now.Add(d)
	s.fireDue()
	return nil
}

// fireDue must be called with s.mu held. It fires every pending timer whose
// deadline is at or before s.now, in (deadline, creation-order), delivering
// each one's own deadline rather than s.now. Every fired timer is dropped
// from s.timers so a long-running simulation cannot accumulate timers it
// has already delivered.
func (s *Simulated) fireDue() {
	sort.Slice(s.timers, func(i, j int) bool {
		a, b := s.timers[i], s.timers[j]
		if a.deadline.Equal(b.deadline) {
			return a.seq < b.seq
		}
		return a.deadline.Before(b.deadline)
	})

	remaining := s.timers[:0]
	for _, t := range s.timers {
		if t.deadline.After(s.now) {
			remaining = append(remaining, t)
			continue
		}
		t.fired = true
		select {
		case t.c <- t.deadline:
		default:
		}
	}
	s.timers = remaining
}

// removeTimer drops t from the active timer set. Called by simTimer.Stop,
// with s.mu already held by the caller.
func (s *Simulated) removeTimer(t *simTimer) {
	for i, existing := range s.timers {
		if existing == t {
			s.timers = append(s.timers[:i], s.timers[i+1:]...)
			return
		}
	}
}

// simTimer is Simulated's Timer implementation. Its stopped and fired
// fields are guarded by clock.mu rather than a mutex of their own: every
// access happens either from a Simulated method that already holds it
// (fireDue, via Advance) or from Stop, which acquires it itself. This keeps
// timer state and the active-timer set consistent under one lock instead of
// risking the two disagreeing under concurrent Advance and Stop calls.
type simTimer struct {
	clock    *Simulated
	deadline time.Time
	seq      uint64
	c        chan time.Time

	stopped bool
	fired   bool
}

func (t *simTimer) C() <-chan time.Time {
	return t.c
}

func (t *simTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	if t.fired || t.stopped {
		return false
	}
	t.stopped = true
	t.clock.removeTimer(t)
	return true
}
