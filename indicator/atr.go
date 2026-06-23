package indicator

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
)

// ATR computes the Average True Range (Wilder) over candle OHLC.
//
// Warmup: needs N candle-to-candle periods (N+1 candles) before Ready() is true.
// ATR keeps fixed-point price units internally; Float64() is for display.
type ATR struct {
	n     int
	scale market.Scale6
	name  string

	prev    market.Candle
	hasPrev bool
	ready   bool
	value   market.PriceSum
	periods int
	sumTR   market.PriceSum
}

func NewATR(period int, scale market.Scale6) (*ATR, error) {
	if period <= 0 {
		return nil, fmt.Errorf("ATR period must be > 0")
	}
	if scale <= 0 {
		return nil, fmt.Errorf("ATR scale must be > 0")
	}
	return &ATR{
		n:     period,
		scale: scale,
		name:  fmt.Sprintf("ATR(%d)", period),
	}, nil
}

func (a *ATR) Name() string              { return a.name }
func (a *ATR) Period() int               { return a.n }
func (a *ATR) Warmup() int               { return a.n + 1 } // N periods = N+1 candles
func (a *ATR) Ready() bool               { return a.ready }
func (a *ATR) PriceSum() market.PriceSum { return a.value }
func (a *ATR) Price() market.Price       { return market.Price(a.value) }
func (a *ATR) Float64() float64          { return priceToFloat64(int64(a.value), a.scale) }

func (a *ATR) Reset() {
	*a = ATR{n: a.n, scale: a.scale, name: a.name}
}

func (a *ATR) Update(c market.Candle) {
	if !a.hasPrev {
		a.prev = c
		a.hasPrev = true
		return
	}

	tr := max3Int64(
		int64(c.High-c.Low),
		absPriceDiff(c.High, a.prev.Close),
		absPriceDiff(c.Low, a.prev.Close),
	)

	a.periods++
	if a.periods < a.n {
		a.sumTR += market.PriceSum(tr)
	} else if a.periods == a.n {
		a.sumTR += market.PriceSum(tr)
		a.value = market.PriceSum(roundDivPositive(int64(a.sumTR), int64(a.n)))
		a.ready = true
	} else {
		a.value = market.PriceSum(roundDivPositive(int64(a.value)*int64(a.n-1)+tr, int64(a.n)))
	}

	a.prev = c
}

var _ PriceIndicator = (*ATR)(nil)
