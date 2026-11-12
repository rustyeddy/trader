package trader

import "github.com/rustyeddy/trader/num"

// ema is an exponential moving average of the close, seeded with the SMA
// of the first window closes.
type ema struct {
	window int
	alpha  float64
	count  int
	sum    float64 // warmup only
	value  float64
}

func newEMA(window int) *ema {
	if window <= 0 {
		panic("ema: window must be > 0")
	}
	return &ema{window: window, alpha: 2 / float64(window+1)}
}

// onBar returns the current EMA, and false until a full window has been
// seen to seed it.
func (e *ema) onBar(b bar) (float64, bool) {
	val := b.c.Float64()
	if e.count < e.window {
		e.count++
		e.sum += val
		if e.count < e.window {
			return 0, false
		}
		e.value = e.sum / float64(e.window)
		return e.value, true
	}
	e.value += e.alpha * (val - e.value)
	return e.value, true
}

// atr is the average true range: the simple average of the last window true
// ranges, not Wilder's recursive smoothing, so a value depends only on the last
// window+1 bars. The true ranges are kept as exact prices; only the reported
// average is a float (the ADR-045 boundary).
type atr struct {
	ranges []num.Price // the last window true ranges, as a ring
	next   int         // the slot the next true range goes in
	sum    num.Price   // of ranges
	bars   int         // seen so far, up to window+1
	prev   num.Price   // the previous bar's close
}

// maxATRWindow keeps the sum of true ranges, each at most maxPrice, far inside
// the range of a Price.
const maxATRWindow = 1_000_000

func newATR(window int) *atr {
	if window <= 0 {
		panic("atr: window must be > 0")
	}
	if window > maxATRWindow {
		panic("atr: window is too large")
	}
	return &atr{ranges: make([]num.Price, window)}
}

// onBar returns the current ATR, in price units, and false until window+1 bars
// have been seen: a true range needs a previous close, so the first bar only
// supplies one.
func (a *atr) onBar(b bar) (float64, bool) {
	prev := a.prev
	a.prev = b.c
	if a.bars == 0 {
		a.bars = 1
		return 0, false
	}

	// The true range is the largest of the bar's range and how far it reached
	// past the previous close on either side. Each term is a distance, so a bar
	// that stays on one side of the close contributes nothing for the other.
	tr := priceMax(mustDiff(b.h, b.l), above(b.h, prev), above(prev, b.l))
	sum, err := a.sum.Add(tr)
	if err == nil {
		sum, err = sum.Sub(a.ranges[a.next])
	}
	if err != nil {
		panic("atr: sum of true ranges left the price range: " + err.Error()) // window and prices are bounded
	}
	a.sum = sum
	a.ranges[a.next] = tr
	a.next = (a.next + 1) % len(a.ranges)

	window := len(a.ranges)
	if a.bars <= window {
		a.bars++
	}
	if a.bars <= window {
		return 0, false
	}
	return a.sum.Float64() / float64(window), true
}

// above is how far a is above b, or zero when it is not.
func above(a, b num.Price) num.Price {
	if a.Cmp(b) <= 0 {
		return num.Price{}
	}
	d, err := a.Sub(b)
	if err != nil {
		panic("above: " + err.Error()) // a > b, so the difference is not negative
	}
	return d
}

// mustDiff is hi - lo for a bar's high and low, which parseBar guarantees are
// ordered.
func mustDiff(hi, lo num.Price) num.Price { return above(hi, lo) }
