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
