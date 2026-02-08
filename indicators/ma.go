package indicators

import (
	"fmt"

	"github.com/rustyeddy/trader/pricing"
)

// MA calculates the Simple Moving Average for the given period.
// Returns an error if there aren't enough candles for the period.
func MA(candles []pricing.Candle, period int) (float64, error) {
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
func EMA(candles []pricing.Candle, period int) (float64, error) {
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
