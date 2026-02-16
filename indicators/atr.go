package indicators

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
)

// ATRFunc calculates the Average True Range (Wilder) for the given period.
//
// IMPORTANT: prices are assumed to be fixed-point int32 (scaled). The returned
// value is in the SAME scaled units (e.g. if scale=1e6, ATR=200 means 0.000200).
func ATRFunc(candles []market.Candle, period int) (int32, error) {
	if period <= 0 {
		return 0, fmt.Errorf("period must be positive, got %d", period)
	}
	if len(candles) < period+1 {
		return 0, fmt.Errorf("not enough candles: need %d, got %d", period+1, len(candles))
	}

	// Initial ATR: SMA of first 'period' true ranges
	var sumTR int64
	for i := 1; i <= period; i++ {
		sumTR += int64(trueRange(candles[i], candles[i-1]))
	}
	atr := sumTR / int64(period)

	// Wilder smoothing for remaining true ranges
	p := int64(period)
	for i := period + 1; i < len(candles); i++ {
		tr := int64(trueRange(candles[i], candles[i-1]))
		atr = (atr*(p-1) + tr) / p
	}

	return int32(atr), nil
}

// ATR is a streaming Average True Range (Wilder) indicator.
//
// Value() returns ATR in scaled price units (same scaling as input candles).
type ATR struct {
	period      int
	atr         int32
	count       int
	warmupSum   int64
	prevCandle  market.Candle
	hasPrevious bool
}

func NewATR(period int) *ATR {
	return &ATR{period: period}
}

func (a *ATR) Name() string { return fmt.Sprintf("ATR(%d)", a.period) }

func (a *ATR) Warmup() int {
	// Need period+1 candles because TR requires previous candle
	return a.period + 1
}

func (a *ATR) Reset() {
	a.atr = 0
	a.count = 0
	a.warmupSum = 0
	a.hasPrevious = false
}

func (a *ATR) Update(c market.Candle) {
	if !a.hasPrevious {
		a.prevCandle = c
		a.hasPrevious = true
		return
	}

	tr := int64(trueRange(c, a.prevCandle))

	if a.count < a.period {
		a.warmupSum += tr
		a.count++
		if a.count == a.period {
			a.atr = int32(a.warmupSum / int64(a.period))
		}
	} else {
		p := int64(a.period)
		atr64 := int64(a.atr)
		a.atr = int32((atr64*(p-1) + tr) / p)
	}

	a.prevCandle = c
}

func (a *ATR) Ready() bool { return a.count >= a.period }

func (a *ATR) Value() float64 {
	if !a.Ready() {
		return 0
	}
	// Indicator interface expects float64; we return scaled units as float64.
	return float64(a.atr)
}

// trueRange calculates the True Range for a candle given the previous candle.
// Returns TR in scaled price units.
func trueRange(cur, prev market.Candle) int32 {
	highLow := int64(cur.H - cur.L)
	highClose := abs64(int64(cur.H) - int64(prev.C))
	lowClose := abs64(int64(cur.L) - int64(prev.C))

	tr := max64(highLow, max64(highClose, lowClose))
	if tr < 0 {
		tr = 0
	}
	if tr > int64(int32(^uint32(0)>>1)) {
		return int32(^uint32(0) >> 1)
	}
	return int32(tr)
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
