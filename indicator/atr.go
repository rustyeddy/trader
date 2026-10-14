package indicator

import "math"

// TrueRange is Wilder's true range for one bar: the largest of the bar's
// own range (high-low) and its gaps from the previous close
// (high-prevClose and prevClose-low).
func TrueRange(high, low, prevClose float64) float64 {
	tr := high - low
	if up := high - prevClose; up > tr {
		tr = up
	}
	if down := prevClose - low; down > tr {
		tr = down
	}
	return tr
}

// ATR is a streaming average true range over a fixed period (issue #506):
// the simple average of the last Period true ranges.
//
// # Simple average, not Wilder smoothing
//
// Wilder's original ATR smooths recursively, so its value depends on every
// bar ever supplied. ATR here is the simple average of exactly the last
// Period true ranges, so its value is a pure function of the last Period+1
// bars: any two ATRs whose last Period+1 bars are identical report
// bit-identical values, however different their earlier history. To
// guarantee that, ATR keeps the retained true ranges in a ring and sums
// them oldest to newest each time Value is called. It deliberately does
// not use the incremental running mean SMA uses, whose floating-point
// rounding would carry a trace of evicted samples. The cost is O(Period)
// per Value call. That is the convention the
// forex-atr scanner ranks by; RSI is this package's Wilder-smoothed
// indicator, and a Wilder ATR would be a separate type.
//
// # Warmup
//
// Each true range needs the previous bar's close, so the first Update only
// establishes it and produces no true range. Ready becomes true on the
// (Period+1)-th Update.
//
// The zero value is not usable; construct an ATR with NewATR.
type ATR struct {
	period    int
	trs       []float64 // ring of the most recent true ranges
	pos       int       // next slot to write; the oldest once the ring is full
	filled    bool
	haveClose bool
	prevClose float64
}

// NewATR returns an ATR of the given period. period must be positive;
// NewATR returns ErrInvalidPeriod otherwise.
func NewATR(period int) (*ATR, error) {
	if period <= 0 {
		return nil, ErrInvalidPeriod
	}
	return &ATR{period: period, trs: make([]float64, period)}, nil
}

// Period returns the period this ATR was constructed with.
func (a *ATR) Period() int {
	return a.period
}

// Update advances a by one bar. It rejects a non-finite high, low or
// close with ErrNonFiniteSample and leaves a's state unchanged, the same
// contract EMA.Update documents.
func (a *ATR) Update(high, low, close float64) error {
	for _, v := range [...]float64{high, low, close} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return ErrNonFiniteSample
		}
	}
	if a.haveClose {
		a.trs[a.pos] = TrueRange(high, low, a.prevClose)
		a.pos++
		if a.pos == a.period {
			a.pos, a.filled = 0, true
		}
	}
	a.prevClose, a.haveClose = close, true
	return nil
}

// Ready reports whether a has seen Period+1 bars, enough for Period true
// ranges. Callers must check Ready before trusting Value.
func (a *ATR) Ready() bool {
	return a.filled
}

// Value returns the average of the last Period true ranges. It is
// meaningful only once Ready reports true; before then it returns 0.
func (a *ATR) Value() float64 {
	if !a.filled {
		return 0
	}
	// Sum oldest to newest: a.pos is the oldest slot once the ring is full.
	var sum float64
	for i := range a.period {
		sum += a.trs[(a.pos+i)%a.period]
	}
	return sum / float64(a.period)
}
