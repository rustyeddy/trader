package indicator

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// EMA computes an Exponential Moving Average over candle closes.
//
// Pricing note:
//   - market.Candle prices are scaled integers.
//   - EMA stores scaled price units internally; Float64 is only for display.
type EMA struct {
	n     int
	scale types.Scale6

	seen  int
	value types.PriceSum
	ready bool

	name string
}

func NewEMA(period int, scale types.Scale6) (*EMA, error) {
	if period <= 0 {
		return nil, fmt.Errorf("EMA period must be > 0")
	}
	if scale <= 0 {
		return nil, fmt.Errorf("EMA scale must be > 0")
	}
	return &EMA{
		n:     period,
		scale: scale,
		name:  fmt.Sprintf("EMA(%d)", period),
	}, nil
}

func (e *EMA) Name() string             { return e.name }
func (e *EMA) Period() int              { return e.n }
func (e *EMA) Warmup() int              { return e.n }
func (e *EMA) Ready() bool              { return e.ready }
func (e *EMA) PriceSum() types.PriceSum { return e.value }
func (e *EMA) Price() types.Price       { return types.Price(e.value) }
func (e *EMA) Float64() float64         { return float64(e.value) / float64(e.scale) }

func (e *EMA) Reset() {
	e.seen = 0
	e.value = 0
	e.ready = false
}

func (e *EMA) Update(c market.Candle) {
	e.seen++
	if e.seen == 1 {
		// Seed with the first close (simple, deterministic).
		e.value = types.PriceSum(c.Close)
	} else {
		denom := types.PriceSum(e.n + 1)
		e.value = (types.PriceSum(c.Close)*2 + e.value*types.PriceSum(e.n-1) + denom/2) / denom
	}

	if e.seen >= e.n {
		e.ready = true
	}
}

var _ PriceIndicator = (*EMA)(nil)
