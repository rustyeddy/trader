package indicators

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/market"
)

// SimpleMA is a streaming Simple Moving Average indicator
type SimpleMA struct {
	period  int
	candles []market.Candle
}

// NewMA creates a new Simple Moving Average indicator with the given period
func NewMA(period int) *SimpleMA {
	return &SimpleMA{
		period:  period,
		candles: make([]market.Candle, 0, period),
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

func (m *SimpleMA) Update(c market.Candle) {
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

func (e *ExponentialMA) Update(c market.Candle) {
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

// ATR is a streaming Average True Range indicator
type ATR struct {
	period      int
	atr         float64
	count       int
	warmupSum   float64
	prevCandle  market.Candle
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

func (a *ATR) Update(c market.Candle) {
	if !a.hasPrevious {
		// First candle, just store it
		a.prevCandle = c
		a.hasPrevious = true
		return
	}

	// Calculate true range
	tr := calculateTrueRange(c, a.prevCandle)

	if a.count < a.period {
		// During warmup, accumulate sum for initial ATR
		a.warmupSum += tr
		a.count++
		if a.count == a.period {
			// Initialize ATR with average of true ranges
			a.atr = a.warmupSum / float64(a.period)
		}
	} else {
		// Apply Wilder's smoothing
		a.atr = (a.atr*float64(a.period-1) + tr) / float64(a.period)
	}

	a.prevCandle = c
}

func (a *ATR) Ready() bool {
	return a.count >= a.period
}

func (a *ATR) Value() float64 {
	if !a.Ready() {
		return 0
	}
	return a.atr
}

// calculateTrueRange calculates the True Range for a candle given the previous candle
func calculateTrueRange(current, previous market.Candle) float64 {
	highLow := current.High - current.Low
	highClose := math.Abs(current.High - previous.Close)
	lowClose := math.Abs(current.Low - previous.Close)

	return math.Max(highLow, math.Max(highClose, lowClose))
}
