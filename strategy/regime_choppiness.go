package strategy

import (
	"fmt"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

const maxChoppinessThreshold = 100.0

// ChoppinessFilter gates entries using the Choppiness Index.
// When CI < threshold the market is trending; entries are allowed.
// When CI >= threshold the market is ranging; new opens are suppressed.
// The conventional threshold is 61.8. Trending() returns true before Ready()
// as a defensive contract, although the main callers already gate on Ready()
// before consulting the regime state.
type ChoppinessFilter struct {
	period    int
	ci        *indicator.ChoppinessIndex
	threshold float64
}

func NewChoppinessFilter(period int, threshold float64, scale types.Scale6) (*ChoppinessFilter, error) {
	if err := validateChoppinessThreshold(threshold); err != nil {
		return nil, err
	}
	ci, err := indicator.NewChoppinessIndex(period, scale)
	if err != nil {
		return nil, err
	}
	return &ChoppinessFilter{
		period:    period,
		ci:        ci,
		threshold: threshold,
	}, nil
}

func (f *ChoppinessFilter) Name() string {
	return fmt.Sprintf("Choppiness(%d,%.1f)", f.period, f.threshold)
}

func (f *ChoppinessFilter) Ready() bool           { return f.ci.Ready() }
func (f *ChoppinessFilter) Tick(ct market.Candle) { f.ci.Update(ct) }

func (f *ChoppinessFilter) Trending() bool {
	if !f.ci.Ready() {
		return true // don't gate during warmup
	}
	return f.ci.Float64() < f.threshold
}

func (f *ChoppinessFilter) AllowSide(_ types.Side) bool { return true }

// Choppiness exposes the raw CI value for logging/debugging.
func (f *ChoppinessFilter) Choppiness() float64 { return f.ci.Float64() }

// Value exposes the raw CI value for logging/debugging.
func (f *ChoppinessFilter) Value() float64 { return f.Choppiness() }

func validateChoppinessThreshold(threshold float64) error {
	if threshold <= 0 || threshold > maxChoppinessThreshold {
		return fmt.Errorf("choppiness threshold must be > 0 and <= %.0f, got %.2f",
			maxChoppinessThreshold, threshold)
	}
	return nil
}
