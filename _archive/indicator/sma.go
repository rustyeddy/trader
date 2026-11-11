package indicator

import "math"

// SMA is a streaming simple moving average over a fixed rolling window
// of Period samples (issue #279, MR-02). Unlike EMA, an SMA gives every
// sample within its window equal weight and none to any sample outside
// it — the window's own contents fully determine Value, not a
// decaying influence from every sample ever supplied.
//
// The zero value is not usable; construct an SMA with NewSMA.
type SMA struct {
	w *window
}

// NewSMA returns an SMA of the given period. period must be positive;
// NewSMA returns ErrInvalidPeriod otherwise.
func NewSMA(period int) (*SMA, error) {
	if period <= 0 {
		return nil, ErrInvalidPeriod
	}
	return &SMA{w: newWindow(period)}, nil
}

// Period returns the period this SMA was constructed with.
func (s *SMA) Period() int {
	return s.w.period
}

// Update advances s by one new sample, folding it into the rolling
// window (see window's own doc comment for the inclusive-window
// convention and eviction mechanics). Ready becomes true once exactly
// Period samples have been supplied; every Update after that evicts
// the oldest sample in the window as it admits the new one.
//
// Update rejects a non-finite sample (NaN, +Inf, -Inf) with
// ErrNonFiniteSample and leaves s's state unchanged — the same
// contract EMA.Update documents, for the same reason: silently
// accepting one would permanently poison the rolling mean/variance.
func (s *SMA) Update(sample float64) error {
	if math.IsNaN(sample) || math.IsInf(sample, 0) {
		return ErrNonFiniteSample
	}
	s.w.update(sample)
	return nil
}

// Ready reports whether s has received enough samples (Period) to
// produce a meaningful value. Callers must check Ready before trusting
// Value — see EMA.Ready's own doc comment for why.
func (s *SMA) Ready() bool {
	return s.w.ready()
}

// Value returns s's current simple moving average. It is meaningful
// only once Ready reports true; before then it returns 0, which is
// otherwise indistinguishable from a genuine zero-valued average.
func (s *SMA) Value() float64 {
	if !s.w.ready() {
		return 0
	}
	return s.w.mean
}
