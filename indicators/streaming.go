package indicators

import (
	"fmt"

	"github.com/rustyeddy/trader/pricing"
)

// SimpleMA is a streaming Simple Moving Average indicator
type SimpleMA struct {
	period  int
	candles []pricing.Candle
}

// NewMA creates a new Simple Moving Average indicator with the given period
func NewMA(period int) *SimpleMA {
	return &SimpleMA{
		period:  period,
		candles: make([]pricing.Candle, 0, period),
	}
}

func (m *SimpleMA) Name() string {
	return fmt.Sprintf("MA(%d)", m.period)
}

func (m *SimpleMA) Warmup() int {
	return m.period
}

func (m *SimpleMA) Reset() {
	m.candles = m.candles[:0]
}

func (m *SimpleMA) Update(c pricing.Candle) {
	m.candles = append(m.candles, c)
	// Keep only the last 'period' candles
	if len(m.candles) > m.period {
		m.candles = m.candles[1:]
	}
}

func (m *SimpleMA) Ready() bool {
	return len(m.candles) >= m.period
}

func (m *SimpleMA) Value() float64 {
	if !m.Ready() {
		return 0
	}

	sum := 0.0
	for _, candle := range m.candles {
		sum += candle.Close
	}
	return sum / float64(len(m.candles))
}

// ExponentialMA is a streaming Exponential Moving Average indicator
type ExponentialMA struct {
	period     int
	multiplier float64
	ema        float64
	count      int
	warmupSum  float64
}

// NewEMA creates a new Exponential Moving Average indicator with the given period
func NewEMA(period int) *ExponentialMA {
	return &ExponentialMA{
		period:     period,
		multiplier: 2.0 / float64(period+1),
	}
}

func (e *ExponentialMA) Name() string {
	return fmt.Sprintf("EMA(%d)", e.period)
}

func (e *ExponentialMA) Warmup() int {
	return e.period
}

func (e *ExponentialMA) Reset() {
	e.ema = 0
	e.count = 0
	e.warmupSum = 0
}

func (e *ExponentialMA) Update(c pricing.Candle) {
	if e.count < e.period {
		// During warmup, accumulate sum for initial SMA
		e.warmupSum += c.Close
		e.count++
		if e.count == e.period {
			// Initialize EMA with SMA
			e.ema = e.warmupSum / float64(e.period)
		}
	} else {
		// Apply EMA formula
		e.ema = (c.Close-e.ema)*e.multiplier + e.ema
	}
}

func (e *ExponentialMA) Ready() bool {
	return e.count >= e.period
}

func (e *ExponentialMA) Value() float64 {
	if !e.Ready() {
		return 0
	}
	return e.ema
}
