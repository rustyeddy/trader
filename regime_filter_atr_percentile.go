package trader

import "fmt"

// ATRPercentileFilter gates entries based on the percentile rank of the current
// ATR(atrPeriod) within a rolling window of windowSize ATR readings.
//
// Trending() returns false when the current ATR percentile is below threshold,
// indicating a low-volatility ranging regime where breakout entries tend to
// fail. AllowSide() always returns true — this is a regime gate, not directional.
//
// Default params: atrPeriod=20, windowSize=200, threshold=20.0.
// Registered in the factory as "atr-percentile".
type ATRPercentileFilter struct {
	atr        *ATR
	atrPeriod  int
	windowSize int
	threshold  float64

	window []float64 // ring buffer of recent ATR readings
	pos    int       // next write index
	count  int       // readings accumulated so far (capped at windowSize)
}

func NewATRPercentileFilter(atrPeriod, windowSize int, threshold float64, scale Scale6) (*ATRPercentileFilter, error) {
	atr, err := NewATR(atrPeriod, scale)
	if err != nil {
		return nil, err
	}
	return &ATRPercentileFilter{
		atr:        atr,
		atrPeriod:  atrPeriod,
		windowSize: windowSize,
		threshold:  threshold,
		window:     make([]float64, windowSize),
	}, nil
}

func (f *ATRPercentileFilter) Name() string {
	return fmt.Sprintf("ATRPercentile(%d,%d,%.0f)", f.atrPeriod, f.windowSize, f.threshold)
}

func (f *ATRPercentileFilter) Ready() bool { return f.atr.Ready() && f.count > 0 }

func (f *ATRPercentileFilter) Tick(ct CandleTime) {
	f.atr.Update(ct.Candle)
	if !f.atr.Ready() {
		return
	}
	v := f.atr.Float64()
	f.window[f.pos] = v
	f.pos = (f.pos + 1) % f.windowSize
	if f.count < f.windowSize {
		f.count++
	}
}

func (f *ATRPercentileFilter) Trending() bool {
	if !f.Ready() {
		return true
	}
	return f.percentile() >= f.threshold
}

func (f *ATRPercentileFilter) AllowSide(_ Side) bool { return true }

// Percentile exposes the current ATR percentile rank for debugging.
func (f *ATRPercentileFilter) Percentile() float64 { return f.percentile() }

func (f *ATRPercentileFilter) percentile() float64 {
	if f.count == 0 {
		return 100.0
	}
	current := f.atr.Float64()
	below := 0
	for i := 0; i < f.count; i++ {
		if f.window[i] < current {
			below++
		}
	}
	return float64(below) / float64(f.count) * 100.0
}
