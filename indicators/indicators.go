// Package indicators provides technical analysis indicators for trading
package indicators

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/market"
)

// MA calculates the Simple Moving Average for the given period.
// Returns an error if there aren't enough candles for the period.
func MA(candles []market.Candle, period int) (float64, error) {
	if period <= 0 {
		return 0, fmt.Errorf("period must be positive, got %d", period)
	}
	if len(candles) < period {
		return 0, fmt.Errorf("not enough candles: need %d, got %d", period, len(candles))
	}

	sum := 0.0
	for i := len(candles) - period; i < len(candles); i++ {
		sum += candles[i].Close
	}

	return sum / float64(period), nil
}

// EMA calculates the Exponential Moving Average for the given period.
// Returns an error if there aren't enough candles for the period.
func EMA(candles []market.Candle, period int) (float64, error) {
	if period <= 0 {
		return 0, fmt.Errorf("period must be positive, got %d", period)
	}
	if len(candles) < period {
		return 0, fmt.Errorf("not enough candles: need %d, got %d", period, len(candles))
	}

	// Calculate multiplier: 2 / (period + 1)
	multiplier := 2.0 / float64(period+1)

	// Start with SMA for first value
	sma := 0.0
	for i := 0; i < period; i++ {
		sma += candles[i].Close
	}
	ema := sma / float64(period)

	// Calculate EMA for remaining candles
	for i := period; i < len(candles); i++ {
		ema = (candles[i].Close-ema)*multiplier + ema
	}

	return ema, nil
}

// ATR calculates the Average True Range for the given period.
// Returns an error if there aren't enough candles for the period.
func ATR(candles []market.Candle, period int) (float64, error) {
	if period <= 0 {
		return 0, fmt.Errorf("period must be positive, got %d", period)
	}
	if len(candles) < period+1 {
		return 0, fmt.Errorf("not enough candles: need %d, got %d", period+1, len(candles))
	}

	// Calculate true ranges
	trueRanges := make([]float64, 0, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		tr := trueRange(candles[i], candles[i-1])
		trueRanges = append(trueRanges, tr)
	}

	// Calculate initial ATR as SMA of first 'period' true ranges
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += trueRanges[i]
	}
	atr := sum / float64(period)

	// Smooth remaining values using Wilder's method
	for i := period; i < len(trueRanges); i++ {
		atr = (atr*float64(period-1) + trueRanges[i]) / float64(period)
	}

	return atr, nil
}

// trueRange calculates the True Range for a candle given the previous candle
func trueRange(current, previous market.Candle) float64 {
	highLow := current.High - current.Low
	highClose := math.Abs(current.High - previous.Close)
	lowClose := math.Abs(current.Low - previous.Close)

	return math.Max(highLow, math.Max(highClose, lowClose))
}
