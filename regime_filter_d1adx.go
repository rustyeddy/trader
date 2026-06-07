package trader

import "fmt"

// D1ADXFilter is a regime filter that applies ADX at the daily timeframe
// while being fed sub-daily bars (e.g. H1). It aggregates intraday bars into
// daily OHLC and updates the ADX only when a day closes.
//
// IsTrending() returns true when D1 ADX >= threshold, meaning the daily
// timeframe confirms a directional trend. During warmup it returns true to
// avoid suppressing entries before enough data is available.
//
// Registered in the factory as "adx-d1".
type D1ADXFilter struct {
	adx       *ADX
	period    int
	threshold float64

	// Intraday accumulation for the current partial daily bar.
	dayNum   int64 // unix_sec / 86400
	dayOpen  Price
	dayHigh  Price
	dayLow   Price
	dayClose Price
	hasDay   bool
}

func NewD1ADXFilter(period int, threshold float64, scale Scale6) (*D1ADXFilter, error) {
	adx, err := NewADX(period, scale)
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

func (f *D1ADXFilter) Tick(ct CandleTime) {
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
		// Day rolled — finalise the completed daily bar and update ADX.
		f.adx.Update(Candle{
			Open:  f.dayOpen,
			High:  f.dayHigh,
			Low:   f.dayLow,
			Close: f.dayClose,
		})
		// Start fresh accumulation for the new day.
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

func (f *D1ADXFilter) Trending() bool {
	if !f.adx.Ready() {
		return true // don't gate during warmup
	}
	return f.adx.Float64() >= f.threshold
}

func (f *D1ADXFilter) AllowSide(_ Side) bool { return true }

// ADXValue exposes the raw ADX value for debugging.
func (f *D1ADXFilter) ADXValue() float64 { return f.adx.Float64() }
