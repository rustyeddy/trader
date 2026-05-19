package trader

import (
	"fmt"
	"math"
)

// ATR computes the Average True Range (Wilder) over candle OHLC.
//
// Warmup: needs N candle-to-candle periods (N+1 candles) before Ready() is true.
// Output: Float64() returns ATR in price units (same float scale as EMA).
type ATR struct {
	n     int
	scale float64
	name  string

	seen    int
	prev    Candle
	hasPrev bool
	ready   bool
	atr     float64
	periods int
	sumTR   float64 // accumulates first N true ranges to seed Wilder smoothing
}

func NewATR(period int, scale Scale6) *ATR {
	if period <= 0 {
		panic("ATR period must be > 0")
	}
	if scale <= 0 {
		panic("ATR scale must be > 0")
	}
	return &ATR{
		n:     period,
		scale: float64(scale),
		name:  fmt.Sprintf("ATR(%d)", period),
	}
}

func (a *ATR) Name() string     { return a.name }
func (a *ATR) Warmup() int      { return a.n + 1 } // N periods = N+1 candles
func (a *ATR) Ready() bool      { return a.ready }
func (a *ATR) Float64() float64 { return a.atr }

func (a *ATR) Reset() {
	*a = ATR{n: a.n, scale: a.scale, name: a.name}
}

func (a *ATR) Update(c Candle) {
	a.seen++

	if !a.hasPrev {
		a.prev = c
		a.hasPrev = true
		return
	}

	prevC := float64(a.prev.Close) / a.scale
	h := float64(c.High) / a.scale
	l := float64(c.Low) / a.scale

	tr := max3(h-l, math.Abs(h-prevC), math.Abs(l-prevC))

	a.periods++
	if a.periods < a.n {
		a.sumTR += tr
	} else if a.periods == a.n {
		// Seed: simple average of first N true ranges.
		a.sumTR += tr
		a.atr = a.sumTR / float64(a.n)
		a.ready = true
	} else {
		// Wilder smoothing: ATR = (prev*(N-1) + TR) / N
		a.atr = (a.atr*float64(a.n-1) + tr) / float64(a.n)
	}

	a.prev = c
}
