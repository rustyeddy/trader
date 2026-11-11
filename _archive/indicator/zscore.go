package indicator

import "math"

// ZScore is a streaming normalized deviation over a fixed rolling
// window of Period samples (issue #279, MR-02):
//
//	Z = (sample - rolling mean) / rolling population standard deviation
//
// computed from the same window at the same instant — the mean and
// standard deviation Value divides by are always the window as it
// stands immediately after the sample being scored was itself folded
// in (window's own doc comment explains why this inclusive convention
// was chosen over excluding the current sample).
//
// The zero value is not usable; construct a ZScore with NewZScore.
type ZScore struct {
	w *window
}

// NewZScore returns a ZScore of the given period. period must be
// positive; NewZScore returns ErrInvalidPeriod otherwise.
func NewZScore(period int) (*ZScore, error) {
	if period <= 0 {
		return nil, ErrInvalidPeriod
	}
	return &ZScore{w: newWindow(period)}, nil
}

// Period returns the period this ZScore was constructed with.
func (z *ZScore) Period() int {
	return z.w.period
}

// Update advances z by one new sample — see SMA.Update's own doc
// comment for the shared window/eviction mechanics and non-finite-
// sample rejection this type follows identically.
func (z *ZScore) Update(sample float64) error {
	if math.IsNaN(sample) || math.IsInf(sample, 0) {
		return ErrNonFiniteSample
	}
	z.w.update(sample)
	return nil
}

// Ready reports whether z has received enough samples (Period) to
// produce a meaningful value — see SMA.Ready's own doc comment. Ready
// answers only the warm-up question; a Ready ZScore's Value can still
// be invalid for a single observation — see Value's own doc comment.
func (z *ZScore) Ready() bool {
	return z.w.ready()
}

// Value returns z's current Z-score and whether it is valid.
// docs/research/mr-01-experiment-definition.org is explicit that a
// zero-variance window (every sample in the window identical) must
// produce an excluded observation, not NaN or infinity: ok is false in
// exactly that case, and the first return value is 0 rather than a
// division result. Value is meaningful only once Ready reports true;
// calling it before then returns (0, false) for the same reason —
// there is no real window to divide by yet.
func (z *ZScore) Value() (score float64, ok bool) {
	if !z.w.ready() {
		return 0, false
	}
	sd := z.w.stddev()
	if sd == 0 {
		return 0, false
	}
	return (z.w.last - z.w.mean) / sd, true
}
