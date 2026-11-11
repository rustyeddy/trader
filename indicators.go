package main

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
// window+1 bars. The true ranges are kept in exact price points; only the
// reported average is a float, from a single division.
type atr struct {
	ranges []price // the last window true ranges, as a ring
	next   int     // the slot the next true range goes in
	sum    price   // of ranges
	bars   int     // seen so far, up to window+1
	prev   price   // the previous bar's close
}

func newATR(window int) *atr {
	if window <= 0 {
		panic("atr: window must be > 0")
	}
	return &atr{ranges: make([]price, window)}
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

	tr := max(b.h-b.l, b.h-prev, prev-b.l)
	a.sum += tr - a.ranges[a.next]
	a.ranges[a.next] = tr
	a.next = (a.next + 1) % len(a.ranges)

	window := len(a.ranges)
	if a.bars <= window {
		a.bars++
	}
	if a.bars <= window {
		return 0, false
	}
	return float64(a.sum) / float64(int64(window)*priceScale), true
}
