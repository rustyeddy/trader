package main

import "context"

// sma is a simple moving average of the close over a fixed window. It
// sums the integer prices so no rounding error accumulates, and converts
// once when reporting the average.
type sma struct {
	window int
	buf    []price // ring buffer of the last window closes
	next   int     // index of the oldest close / next slot to overwrite
	count  int
	sum    price
}

func newSMA(window int) *sma {
	if window <= 0 {
		panic("sma: window must be > 0")
	}
	return &sma{window: window, buf: make([]price, window)}
}

// onBar returns the average of the last window closes, and false until a
// full window has been seen.
func (s *sma) onBar(_ context.Context, bar ohlc) (float64, bool) {
	if s.count == s.window {
		s.sum -= s.buf[s.next]
	} else {
		s.count++
	}
	s.buf[s.next] = bar.c
	s.sum += bar.c
	s.next = (s.next + 1) % s.window

	if s.count < s.window {
		return 0, false
	}
	return s.sum.Float64() / float64(s.window), true
}

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
func (e *ema) onBar(_ context.Context, bar ohlc) (float64, bool) {
	val := bar.c.Float64()
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
