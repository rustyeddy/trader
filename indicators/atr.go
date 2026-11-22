package indicators

import "github.com/rustyeddy/trader/num"

// maxATRPeriod keeps the sum of true ranges, each at most the largest price, far
// inside the range of a num.Price.
const maxATRPeriod = 1_000_000

// TrueRange is Wilder's true range for one bar: the largest of the bar's own range
// (high-low) and how far it reached past the previous close on either side. Each
// term is a distance, so a bar that stays on one side of the close contributes
// nothing for the other. It is exact: no rounding happens.
func TrueRange(high, low, prevClose num.Price) num.Price {
	return num.Max(above(high, low), above(high, prevClose), above(prevClose, low))
}

// above is how far a is above b, or zero when it is not.
func above(a, b num.Price) num.Price {
	if a.Cmp(b) <= 0 {
		return num.Price{}
	}
	d, err := a.Sub(b)
	if err != nil {
		panic("indicators: above: " + err.Error()) // a > b, so the difference is not negative
	}
	return d
}

// ATR is a streaming average true range over a fixed period: the simple average of
// the last Period true ranges, not Wilder's recursive smoothing, so its value is a
// pure function of the last Period+1 bars however different the history before.
//
// Unlike the other indicators it takes num.Price rather than float64. The true
// ranges and their sum are kept exact, and only the reported average is a float
// (ADR-045): a float sum of float differences would round differently from the
// exact one in the last bits, and ATR results are compared and ranked.
//
// Each true range needs the previous bar's close, so the first Update only
// establishes it. Ready becomes true on the (Period+1)-th Update.
//
// The zero value is not usable; construct an ATR with NewATR.
type ATR struct {
	ranges []num.Price // the last Period true ranges, as a ring
	next   int         // the slot the next true range goes in
	sum    num.Price   // of ranges
	bars   int         // seen so far, up to Period+1
	prev   num.Price   // the previous bar's close
}

// NewATR returns an ATR of the given period. period must be positive and at most
// 1,000,000; NewATR returns ErrInvalidPeriod otherwise.
func NewATR(period int) (*ATR, error) {
	if period <= 0 || period > maxATRPeriod {
		return nil, ErrInvalidPeriod
	}
	return &ATR{ranges: make([]num.Price, period)}, nil
}

// Period returns the period this ATR was constructed with.
func (a *ATR) Period() int {
	return len(a.ranges)
}

// Update advances a by one bar. The error is only that the sum of true ranges left
// the range of a num.Price, which prices within it and a bounded period cannot
// cause; a is unchanged when it is returned.
func (a *ATR) Update(high, low, close num.Price) error {
	if a.bars == 0 {
		a.bars, a.prev = 1, close
		return nil
	}
	tr := TrueRange(high, low, a.prev)
	sum, err := a.sum.Add(tr)
	if err == nil {
		sum, err = sum.Sub(a.ranges[a.next])
	}
	if err != nil {
		return err
	}
	a.sum, a.prev = sum, close
	a.ranges[a.next] = tr
	a.next = (a.next + 1) % len(a.ranges)
	if a.bars <= len(a.ranges) {
		a.bars++
	}
	return nil
}

// Ready reports whether a has seen Period+1 bars, enough for Period true ranges.
func (a *ATR) Ready() bool {
	return a.bars > len(a.ranges)
}

// Value returns the average of the last Period true ranges, in price units. It is
// meaningful only once Ready reports true; before then it returns 0.
func (a *ATR) Value() float64 {
	if !a.Ready() {
		return 0
	}
	return a.sum.Float64() / float64(len(a.ranges))
}
