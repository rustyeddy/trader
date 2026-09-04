package indicator

import "math"

// RollingStdDev is a streaming population standard deviation over a
// fixed rolling window of Period samples (issue #279, MR-02) —
// dispersion of the window's own contents, not an estimate of a wider
// population's variance from a sample of it (see window.variance's own
// doc comment for why population, not sample, variance is used here).
//
// The zero value is not usable; construct a RollingStdDev with
// NewRollingStdDev.
type RollingStdDev struct {
	w *window
}

// NewRollingStdDev returns a RollingStdDev of the given period. period
// must be positive; NewRollingStdDev returns ErrInvalidPeriod
// otherwise.
func NewRollingStdDev(period int) (*RollingStdDev, error) {
	if period <= 0 {
		return nil, ErrInvalidPeriod
	}
	return &RollingStdDev{w: newWindow(period)}, nil
}

// Period returns the period this RollingStdDev was constructed with.
func (r *RollingStdDev) Period() int {
	return r.w.period
}

// Update advances r by one new sample — see SMA.Update's own doc
// comment for the shared window/eviction mechanics and non-finite-
// sample rejection this type follows identically.
func (r *RollingStdDev) Update(sample float64) error {
	if math.IsNaN(sample) || math.IsInf(sample, 0) {
		return ErrNonFiniteSample
	}
	r.w.update(sample)
	return nil
}

// Ready reports whether r has received enough samples (Period) to
// produce a meaningful value — see SMA.Ready's own doc comment.
func (r *RollingStdDev) Ready() bool {
	return r.w.ready()
}

// Value returns r's current rolling population standard deviation. It
// is meaningful only once Ready reports true; before then it returns
// 0, which is otherwise indistinguishable from a genuine zero-variance
// window (every sample in the window identical).
func (r *RollingStdDev) Value() float64 {
	if !r.w.ready() {
		return 0
	}
	return r.w.stddev()
}
