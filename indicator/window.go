package indicator

import "math"

// window is the shared, unexported sliding-window mean/variance
// accumulator behind SMA, RollingStdDev, and ZScore (issue #279,
// MR-02): a fixed-size circular buffer of the most recent Period
// samples, with a running mean and Welford-style M2 (sum of squared
// deviations from the running mean) updated incrementally per sample
// rather than recomputed from scratch.
//
// # Rolling-window semantics (MR-01's own deferred decision)
//
// docs/research/mr-01-experiment-definition.org explicitly defers "the
// exact rolling-window semantics" to this issue. window's answer:
// inclusive — the window backing the Period-th and every later update
// is the most recent Period samples ending at, and including, the
// sample just supplied to update. This matches the universal
// convention for a simple moving average or Bollinger Band (an SMA(20)
// at bar t is computed from bars [t-19, t], not [t-20, t-1]) and keeps
// every type built on window internally consistent with the others.
//
// # Numerical stability
//
// A naive rolling variance — track sum(x) and sum(x^2), then compute
// variance as sum(x^2)/n - mean^2 — loses precision through
// catastrophic cancellation when the samples share a large common
// magnitude and small spread relative to it, exactly FX close prices'
// own shape (for example, a window of EURUSD H1 closes clustered
// around 1.10 with only a few pips of spread). window instead adds and
// removes samples via Welford's incremental mean/M2 update, adapted to
// support removal (subtracting the oldest sample when the window is
// already full) as well as addition — numerically stable regardless of
// the samples' common magnitude, at the same O(1)-per-update cost a
// naive sum/sum-of-squares approach would have anyway.
type window struct {
	period int
	buf    []float64
	pos    int
	filled bool

	n    int
	mean float64
	m2   float64 // sum of squared deviations from mean, Welford-style

	last float64 // the most recently supplied sample
}

func newWindow(period int) *window {
	return &window{period: period, buf: make([]float64, period)}
}

// update advances w by one new sample: if the window is already full,
// the oldest sample (about to be overwritten) is removed from the
// running mean/M2 first, then x is added — net window size stays at
// Period once full, and mean/M2 always reflect exactly the current
// buffer's contents.
func (w *window) update(x float64) {
	if w.filled {
		w.remove(w.buf[w.pos])
	}
	w.add(x)
	w.buf[w.pos] = x
	w.last = x
	w.pos++
	if w.pos == w.period {
		w.pos = 0
		w.filled = true
	}
}

func (w *window) add(x float64) {
	w.n++
	delta := x - w.mean
	w.mean += delta / float64(w.n)
	w.m2 += delta * (x - w.mean)
}

func (w *window) remove(x float64) {
	w.n--
	if w.n == 0 {
		w.mean, w.m2 = 0, 0
		return
	}
	delta := x - w.mean
	w.mean -= delta / float64(w.n)
	w.m2 -= delta * (x - w.mean)
}

// ready reports whether w has accumulated a full Period samples.
func (w *window) ready() bool {
	return w.filled
}

// variance returns w's current population variance (M2 / n, dividing
// by the window's own sample count rather than n-1) — the conventional
// choice for a rolling/Bollinger-style indicator, which describes the
// dispersion of the window's own contents rather than estimating a
// wider population's variance from a sample of it.
//
// True variance can never be negative, but a long sliding replay
// accumulates floating-point roundoff in m2 through remove's repeated
// subtraction, which can in principle leave m2 very slightly negative
// even though the window's real variance is (at or near) zero. Passing
// that straight to stddev's math.Sqrt would produce NaN — exactly the
// silent NaN/Inf issue #279 requires research code never emit — so a
// negative m2 within roundoffEpsilon of zero is treated as zero. A
// negative m2 beyond that tolerance is not roundoff; it indicates a
// real accumulator bug, and is deliberately left unclamped so it
// surfaces as a visible NaN rather than being silently masked.
func (w *window) variance() float64 {
	if w.n == 0 {
		return 0
	}
	m2 := w.m2
	if m2 < 0 && m2 > -roundoffEpsilon {
		m2 = 0
	}
	return m2 / float64(w.n)
}

// roundoffEpsilon bounds the magnitude of negative m2 treated as
// floating-point roundoff rather than a real accumulator bug. It is
// deliberately tiny relative to the scale of real FX price data (a
// squared-deviation term near 1.0), leaving genuine bugs visible as
// NaN rather than silently masked.
const roundoffEpsilon = 1e-9

func (w *window) stddev() float64 {
	return math.Sqrt(w.variance())
}
