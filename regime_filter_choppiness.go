package trader

import "fmt"

// ChoppinessFilter gates entries using the Choppiness Index.
// When CI < threshold the market is trending; entries are allowed.
// When CI >= threshold the market is ranging; new opens are suppressed.
// The conventional threshold is 61.8.
type ChoppinessFilter struct {
	ci        *ChoppinessIndex
	threshold float64
}

func NewChoppinessFilter(period int, threshold float64, scale Scale6) *ChoppinessFilter {
	return &ChoppinessFilter{
		ci:        NewChoppinessIndex(period, scale),
		threshold: threshold,
	}
}

func (f *ChoppinessFilter) Name() string {
	return fmt.Sprintf("Choppiness(%d,%.1f)", f.ci.n, f.threshold)
}

func (f *ChoppinessFilter) Ready() bool   { return f.ci.Ready() }
func (f *ChoppinessFilter) Tick(ct CandleTime) { f.ci.Update(ct.Candle) }

func (f *ChoppinessFilter) Trending() bool {
	if !f.ci.Ready() {
		return true // don't gate during warmup
	}
	return f.ci.Value() < f.threshold
}

// Value exposes the raw CI value for logging/debugging.
func (f *ChoppinessFilter) Value() float64 { return f.ci.Value() }
