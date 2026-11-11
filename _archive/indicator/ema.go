package indicator

import "math"

// EMA is a streaming exponential moving average (issue #248, EMA-03).
// It accepts one new sample at a time via Update; there is no batch
// entry point, since the EMA crossover strategy this package exists
// for consumes bars one at a time as they replay.
//
// EMA uses conventional SMA seeding (the experiment definition's own
// choice, docs/research/ema-01-experiment-definition.org): its first
// Period samples are averaged arithmetically to produce the initial
// EMA value, and only the standard exponential recurrence is applied
// from the following sample onward. Ready reports false until that
// seeding is complete, so a caller cannot mistake a partially-warmed
// average for a real EMA value — see the package doc comment for why
// this matters to the strategy that consumes it.
//
// The zero value is not usable; construct an EMA with NewEMA.
type EMA struct {
	period     int
	multiplier float64

	seedSum   float64
	seedCount int

	value float64
	ready bool
}

// NewEMA returns an EMA of the given period. period must be positive;
// NewEMA returns ErrInvalidPeriod otherwise.
func NewEMA(period int) (*EMA, error) {
	if period <= 0 {
		return nil, ErrInvalidPeriod
	}
	return &EMA{
		period:     period,
		multiplier: 2.0 / (float64(period) + 1),
	}, nil
}

// Period returns the period this EMA was constructed with.
func (e *EMA) Period() int {
	return e.period
}

// Update advances e by one new sample. Before Ready, Update
// accumulates sample into the SMA seed; once exactly Period samples
// have been supplied, Ready becomes true and every Update from then on
// applies the standard EMA recurrence
// (value = sample*multiplier + value*(1-multiplier)) to the
// already-seeded value. Calling Update again after Ready never
// re-seeds: seeding happens exactly once, on the Period-th call.
//
// Update rejects a non-finite sample (NaN, +Inf, -Inf) with
// ErrNonFiniteSample and leaves e's state unchanged, whether e is
// still seeding or already Ready. Silently accepting one would
// permanently poison seedSum/value with a non-finite value — Ready
// could still become true, every later crossover comparison against
// that poisoned value would be false, and the strategy consuming this
// EMA would silently stop producing signals instead of failing at the
// bad input (PR #259 review).
func (e *EMA) Update(sample float64) error {
	if math.IsNaN(sample) || math.IsInf(sample, 0) {
		return ErrNonFiniteSample
	}

	if !e.ready {
		e.seedSum += sample
		e.seedCount++
		if e.seedCount == e.period {
			e.value = e.seedSum / float64(e.period)
			e.ready = true
		}
		return nil
	}
	e.value = sample*e.multiplier + e.value*(1-e.multiplier)
	return nil
}

// Ready reports whether e has received enough samples (Period) to
// produce a meaningful value. Strategy logic must check Ready before
// trusting Value — see the package doc comment.
func (e *EMA) Ready() bool {
	return e.ready
}

// Value returns e's current EMA value. It is meaningful only once
// Ready reports true; before then it returns 0, which is otherwise
// indistinguishable from a genuine zero-valued average — callers must
// consult Ready, not infer readiness from Value.
func (e *EMA) Value() float64 {
	return e.value
}
