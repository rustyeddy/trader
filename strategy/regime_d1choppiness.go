package strategy

import (
	"fmt"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// D1ChoppinessFilter is a regime filter that applies the Choppiness Index at
// the daily timeframe while being fed sub-daily bars (e.g. H1). It aggregates
// intraday bars into daily OHLC and updates the CI only when a day closes.
//
// This avoids the correlation problem that arises when using same-timeframe CI
// with Donchian breakouts: a breakout bar will always look "trending" at the
// moment of entry when measured on its own timeframe. The daily CI captures
// whether the broader market context is trending over multiple days, which is
// independent of any individual H1 breakout signal.
// AllowSide() always returns true because this is a regime gate, not a
// directional filter. Trending() returns true before Ready() as a defensive
// contract, although the main callers already gate on Ready() before
// consulting the regime state.
//
// Registered in the factory as "choppiness-d1".
type D1ChoppinessFilter struct {
	period    int
	ci        *indicator.ChoppinessIndex
	threshold float64

	// Intraday accumulation for the current partial daily bar.
	dailyCandleAccumulator
}

func NewD1ChoppinessFilter(period int, threshold float64, scale types.Scale6) (*D1ChoppinessFilter, error) {
	if err := validateChoppinessThreshold(threshold); err != nil {
		return nil, err
	}
	ci, err := indicator.NewChoppinessIndex(period, scale)
	if err != nil {
		return nil, err
	}
	return &D1ChoppinessFilter{
		period:    period,
		ci:        ci,
		threshold: threshold,
	}, nil
}

func (f *D1ChoppinessFilter) Name() string {
	return fmt.Sprintf("D1-Choppiness(%d,%.1f)", f.period, f.threshold)
}

func (f *D1ChoppinessFilter) Ready() bool { return f.ci.Ready() }

func (f *D1ChoppinessFilter) Tick(ct market.CandleTime) {
	if daily, rolled := f.dailyCandleAccumulator.Tick(ct); rolled {
		f.ci.Update(daily)
	}
}

func (f *D1ChoppinessFilter) Trending() bool {
	if !f.ci.Ready() {
		return true // don't gate during warmup
	}
	return f.ci.Float64() < f.threshold
}

func (f *D1ChoppinessFilter) AllowSide(_ types.Side) bool { return true }

// Choppiness exposes the raw CI value for debugging.
func (f *D1ChoppinessFilter) Choppiness() float64 { return f.ci.Float64() }

// Value exposes the raw CI value for debugging.
func (f *D1ChoppinessFilter) Value() float64 { return f.Choppiness() }
