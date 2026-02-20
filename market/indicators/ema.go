// pkg/indicators/ema.go
package indicators

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
)

// EMA computes an Exponential Moving Average over candle closes.
//
// Pricing note:
//   - market.Candle prices are scaled integers.
//   - EMA outputs float64 in *price units* (e.g. 1.08765), so we need the CandleSet scale.
//     Pass the same scale used to build your CandleSet (e.g. 1_000_000 for Dukascopy).
type EMA struct {
	n     int
	alpha float64
	scale float64

	seen  int
	value float64
	ready bool

	name string
}

func NewEMA(period int, scale int32) *EMA {
	if period <= 0 {
		panic("EMA period must be > 0")
	}
	if scale <= 0 {
		panic("EMA scale must be > 0")
	}
	return &EMA{
		n:     period,
		alpha: 2.0 / float64(period+1),
		scale: float64(scale),
		name:  fmt.Sprintf("EMA(%d)", period),
	}
}

func (e *EMA) Name() string     { return e.name }
func (e *EMA) Warmup() int      { return e.n }
func (e *EMA) Ready() bool      { return e.ready }
func (e *EMA) Float64() float64 { return e.value }

func (e *EMA) Reset() {
	e.seen = 0
	e.value = 0
	e.ready = false
}

func (e *EMA) Update(c market.OHLC) {
	// Convert scaled integer close into float price units.
	x := float64(c.C) / e.scale

	e.seen++
	if e.seen == 1 {
		// Seed with the first close (simple, deterministic).
		e.value = x
	} else {
		e.value = e.alpha*x + (1.0-e.alpha)*e.value
	}

	if e.seen >= e.n {
		e.ready = true
	}
}
