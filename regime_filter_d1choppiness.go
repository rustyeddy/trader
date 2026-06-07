package trader

import "fmt"

// D1ChoppinessFilter is a regime filter that applies the Choppiness Index at
// the daily timeframe while being fed sub-daily bars (e.g. H1). It aggregates
// intraday bars into daily OHLC and updates the CI only when a day closes.
//
// This avoids the correlation problem that arises when using same-timeframe CI
// with Donchian breakouts: a breakout bar will always look "trending" at the
// moment of entry when measured on its own timeframe. The daily CI captures
// whether the broader market context is trending over multiple days, which is
// independent of any individual H1 breakout signal.
//
// Registered in the factory as "choppiness-d1".
type D1ChoppinessFilter struct {
	ci        *ChoppinessIndex
	threshold float64

	// Intraday accumulation for the current partial daily bar.
	dayNum   int64 // unix_sec / 86400 for the bar being accumulated
	dayOpen  Price
	dayHigh  Price
	dayLow   Price
	dayClose Price
	hasDay   bool
}

func NewD1ChoppinessFilter(period int, threshold float64, scale Scale6) (*D1ChoppinessFilter, error) {
	ci, err := NewChoppinessIndex(period, scale)
	if err != nil {
		return nil, err
	}
	return &D1ChoppinessFilter{
		ci:        ci,
		threshold: threshold,
	}, nil
}

func (f *D1ChoppinessFilter) Name() string {
	return fmt.Sprintf("D1-Choppiness(%d,%.1f)", f.ci.n, f.threshold)
}

func (f *D1ChoppinessFilter) Ready() bool { return f.ci.Ready() }

func (f *D1ChoppinessFilter) Tick(ct CandleTime) {
	dayNum := int64(ct.Timestamp) / 86400

	if !f.hasDay {
		f.dayNum = dayNum
		f.dayOpen = ct.Open
		f.dayHigh = ct.High
		f.dayLow = ct.Low
		f.dayClose = ct.Close
		f.hasDay = true
		return
	}

	if dayNum != f.dayNum {
		// Day rolled — finalise the completed daily bar and update CI.
		f.ci.Update(Candle{
			Open:  f.dayOpen,
			High:  f.dayHigh,
			Low:   f.dayLow,
			Close: f.dayClose,
		})
		// Start a fresh accumulation for the new day.
		f.dayNum = dayNum
		f.dayOpen = ct.Open
		f.dayHigh = ct.High
		f.dayLow = ct.Low
		f.dayClose = ct.Close
	} else {
		// Same day — extend the current daily bar.
		if ct.High > f.dayHigh {
			f.dayHigh = ct.High
		}
		if ct.Low < f.dayLow {
			f.dayLow = ct.Low
		}
		f.dayClose = ct.Close
	}
}

func (f *D1ChoppinessFilter) Trending() bool {
	if !f.ci.Ready() {
		return true // don't gate during warmup
	}
	return f.ci.Value() < f.threshold
}

func (f *D1ChoppinessFilter) AllowSide(_ Side) bool { return true }

// Value exposes the raw CI value for debugging.
func (f *D1ChoppinessFilter) Value() float64 { return f.ci.Value() }
