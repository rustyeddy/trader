package indicators

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
)

// MA calculates the Simple Moving Average for the given period.
//
// Prices are fixed-point int32; the returned value is in the same scaled units
// as float64.
func MA(candles []market.Candle, period int) (float64, error) {
	if period <= 0 {
		return 0, fmt.Errorf("period must be positive, got %d", period)
	}
	if len(candles) < period {
		return 0, fmt.Errorf("not enough candles: need %d, got %d", period, len(candles))
	}

	sum := 0.0
	for i := len(candles) - period; i < len(candles); i++ {
		sum += float64(candles[i].C)
	}
	return sum / float64(period), nil
}

// EMA calculates the Exponential Moving Average for the given period.
//
// Prices are fixed-point int32; the returned value is in the same scaled units
// as float64.
func EMA(candles []market.Candle, period int) (float64, error) {
	if period <= 0 {
		return 0, fmt.Errorf("period must be positive, got %d", period)
	}
	if len(candles) < period {
		return 0, fmt.Errorf("not enough candles: need %d, got %d", period, len(candles))
	}

	multiplier := 2.0 / float64(period+1)

	// Start with SMA for first value
	sma := 0.0
	for i := 0; i < period; i++ {
		sma += float64(candles[i].C)
	}
	ema := sma / float64(period)

	for i := period; i < len(candles); i++ {
		closeV := float64(candles[i].C)
		ema = (closeV-ema)*multiplier + ema
	}

	return ema, nil
}
