package indicators

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/pricing"
)

// func trueRange(prevClose, high, low int32) int32 {
// 	a := high - low
// 	b := math.Abs(high - prevClose)
// 	c := math.Abs(low - prevClose)
// 	return math.Max(a, math.Max(b, c))
// }

// ATR calculates the Average True Range for the given period.
// Returns an error if there aren't enough candles for the period.
func ATRFunc(candles []pricing.Candle, period int) (int32, error) {
	if period <= 0 {
		return 0, fmt.Errorf("period must be positive, got %d", period)
	}
	if len(candles) < period+1 {
		return 0, fmt.Errorf("not enough candles: need %d, got %d", period+1, len(candles))
	}

	// Calculate true ranges
	trueRanges := make([]int32, 0, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		tr := trueRange(candles[i], candles[i-1])
		trueRanges = append(trueRanges, tr)
	}

	// Calculate initial ATR as SMA of first 'period' true ranges
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += trueRanges[i]
	}
	atr := sum / int32(period)

	// Smooth remaining values using Wilder's method
	for i := period; i < len(trueRanges); i++ {
		atr = (atr*int32(period-1) + trueRanges[i]) / int32(period)
	}

	return atr, nil
}

// ATR is a streaming Average True Range indicator
type ATR struct {
	period      int
	atr         int32
	count       int
	warmupSum   int32
	prevCandle  pricing.Candle
	hasPrevious bool
}

// NewATR creates a new Average True Range indicator with the given period
func NewATR(period int) *ATR {
	return &ATR{
		period: period,
	}
}

func (a *ATR) Name() string {
	return fmt.Sprintf("ATR(%d)", a.period)
}

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

func (a *ATR) Update(c pricing.Candle) {
	if !a.hasPrevious {
		// First candle, just store it
		a.prevCandle = c
		a.hasPrevious = true
		return
	}

	// Calculate true range
	tr := trueRange(c, a.prevCandle)

	if a.count < a.period {
		// During warmup, accumulate sum for initial ATR
		a.warmupSum += tr
		a.count++
		if a.count == a.period {
			// Initialize ATR with average of true ranges
			a.atr = a.warmupSum / int32(a.period)
		}
	} else {
		// Apply Wilder's smoothing
		a.atr = (a.atr*int32(a.period-1) + tr) / int32(a.period)
	}

	a.prevCandle = c
	return
}

func (a *ATR) Calculate(candles []pricing.Candle) (v int32) {
	for _, c := range candles {
		a.Update(c)
		v = a.Value()
	}
	return v
}

func (a *ATR) Ready() bool {
	return a.count >= a.period
}

func (a *ATR) Value() int32 {
	if !a.Ready() {
		return 0
	}
	return a.atr
}

// trueRange calculates the True Range for a candle given the previous candle
func trueRange(current, previous pricing.Candle) int32 {
	highLow := current.High - current.Low
	highClose := math.Abs(current.High - previous.Close)
	lowClose := math.Abs(current.Low - previous.Close)

	return math.Max(highLow, math.Max(highClose, lowClose))
}
