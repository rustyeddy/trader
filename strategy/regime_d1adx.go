package strategy

import (
	"fmt"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

const maxADXThreshold = 100.0

// D1ADXFilter is a regime filter that applies ADX at the daily timeframe
// while being fed sub-daily bars (e.g. H1). It aggregates intraday bars into
// daily OHLC and updates the ADX only when a day closes.
//
// Trending() returns true when D1 ADX >= threshold, meaning the daily
// timeframe confirms a broad enough trend to allow new entries. AllowSide()
// always returns true because this is a regime gate, not a directional filter.
// Trending() returns true before Ready() as a defensive contract, although the
// main callers already gate on Ready() before consulting the regime state.
//
// Registered in the factory as "adx-d1".
type D1ADXFilter struct {
	adx       *indicator.ADX
	period    int
	threshold float64

	// Intraday accumulation for the current partial daily bar.
	dailyCandleAccumulator
}

func NewD1ADXFilter(period int, threshold float64, scale types.Scale6) (*D1ADXFilter, error) {
	if err := validateADXThreshold(threshold); err != nil {
		return nil, err
	}
	adx, err := indicator.NewADX(period, scale)
	if err != nil {
		return nil, err
	}
	return &D1ADXFilter{
		adx:       adx,
		period:    period,
		threshold: threshold,
	}, nil
}

func (f *D1ADXFilter) Name() string {
	return fmt.Sprintf("D1-ADX(%d,%.1f)", f.period, f.threshold)
}

func (f *D1ADXFilter) Ready() bool { return f.adx.Ready() }

func (f *D1ADXFilter) Tick(ct market.Candle) {
	if daily, rolled := f.dailyCandleAccumulator.Tick(ct); rolled {
		f.adx.Update(daily)
	}
}

func (f *D1ADXFilter) Trending() bool {
	if !f.adx.Ready() {
		return true // don't gate during warmup
	}
	return f.adx.Float64() >= f.threshold
}

func (f *D1ADXFilter) AllowSide(_ types.Side) bool { return true }

// ADX exposes the raw ADX value for debugging.
func (f *D1ADXFilter) ADX() float64 { return f.adx.Float64() }

// ADXValue exposes the raw ADX value for debugging.
func (f *D1ADXFilter) ADXValue() float64 { return f.ADX() }

func validateADXThreshold(threshold float64) error {
	if threshold < 0 || threshold > maxADXThreshold {
		return fmt.Errorf("ADX threshold must be between 0 and %.0f, got %.2f",
			maxADXThreshold, threshold)
	}
	return nil
}
