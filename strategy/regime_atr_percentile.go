package strategy

import (
	"fmt"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
)

const (
	minATRPercentileThreshold = 0.0
	maxATRPercentileThreshold = 100.0
)

// ATRPercentileFilter gates entries based on the percentile rank of the current
// ATR(atrPeriod) within a rolling window of windowSize ATR readings.
//
// Trending() returns false when the current ATR percentile is below threshold,
// indicating a low-volatility ranging regime where breakout entries tend to
// fail. AllowSide() always returns true — this is a regime gate, not directional.
// Ready() becomes true as soon as ATR has warmed up and the first ATR reading
// has been recorded into the rolling window; the window does not need to be
// completely full before the filter starts classifying bars.
//
// Default params: atrPeriod=20, windowSize=200, threshold=20.0.
// Registered in the factory as "atr-percentile".
type ATRPercentileFilter struct {
	atr        *indicator.ATR
	atrPeriod  int
	windowSize int
	threshold  float64

	window []market.Price // ring buffer of recent ATR readings
	pos    int            // next write index
	count  int            // readings accumulated so far (capped at windowSize)
}

func NewATRPercentileFilter(atrPeriod, windowSize int, threshold float64, scale market.Scale6) (*ATRPercentileFilter, error) {
	if windowSize <= 0 {
		return nil, fmt.Errorf("ATR percentile window size must be > 0")
	}
	if threshold < minATRPercentileThreshold || threshold > maxATRPercentileThreshold {
		return nil, fmt.Errorf("ATR percentile threshold must be between %.0f and %.0f, got %.2f",
			minATRPercentileThreshold, maxATRPercentileThreshold, threshold)
	}
	atr, err := indicator.NewATR(atrPeriod, scale)
	if err != nil {
		return nil, err
	}
	return &ATRPercentileFilter{
		atr:        atr,
		atrPeriod:  atrPeriod,
		windowSize: windowSize,
		threshold:  threshold,
		window:     make([]market.Price, windowSize),
	}, nil
}

func (f *ATRPercentileFilter) Name() string {
	return fmt.Sprintf("ATRPercentile(%d,%d,%.0f)", f.atrPeriod, f.windowSize, f.threshold)
}

func (f *ATRPercentileFilter) Ready() bool { return f.atr.Ready() && f.count > 0 }

func (f *ATRPercentileFilter) Tick(ct market.CandleTime) {
	f.atr.Update(ct.Candle)
	if !f.atr.Ready() {
		return
	}
	v := f.atr.Price()
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

func (f *ATRPercentileFilter) AllowSide(_ market.Side) bool { return true }

// Percentile exposes the current ATR percentile rank for debugging.
// Equal ATR values share the middle of their tie bucket, so a completely flat
// ATR window reports the 50th percentile instead of collapsing to 0.
func (f *ATRPercentileFilter) Percentile() float64 { return f.percentile() }

func (f *ATRPercentileFilter) percentile() float64 {
	if f.count == 0 {
		return 100.0
	}
	current := f.atr.Price()
	below := 0
	equal := 0
	for i := 0; i < f.count; i++ {
		switch {
		case f.window[i] < current:
			below++
		case f.window[i] == current:
			equal++
		}
	}
	// Use average tie rank so a flat window sits at the 50th percentile rather
	// than collapsing to 0 simply because the current ATR equals prior values.
	return float64(2*below+equal) * 50.0 / float64(f.count)
}
