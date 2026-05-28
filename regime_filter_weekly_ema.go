package trader

import (
	"fmt"
	"time"
)

// WeeklyEMAFilter is a directional regime filter that aggregates sub-daily bars
// into ISO weekly bars and runs an EMA(period) over weekly closes.
//
// Trending() always returns true — this is a direction-only filter.
// AllowSide(Long)  returns true when the latest weekly close is above the EMA.
// AllowSide(Short) returns true when the latest weekly close is below the EMA.
//
// During warmup (EMA not yet ready) AllowSide returns true so no entries are
// suppressed before enough weekly data has accumulated.
//
// Registered in the factory as "weekly-ema".
type WeeklyEMAFilter struct {
	ema    *EMA
	period int
	scale  float64

	// Weekly bar accumulation.
	isoYear int
	isoWeek int
	wOpen   Price
	wHigh   Price
	wLow    Price
	wClose  Price
	hasWeek bool
}

func NewWeeklyEMAFilter(period int, scale Scale6) *WeeklyEMAFilter {
	return &WeeklyEMAFilter{
		ema:    NewEMA(period, scale),
		period: period,
		scale:  float64(scale),
	}
}

func (f *WeeklyEMAFilter) Name() string {
	return fmt.Sprintf("WeeklyEMA(%d)", f.period)
}

func (f *WeeklyEMAFilter) Ready() bool { return f.ema.Ready() }

func (f *WeeklyEMAFilter) Tick(ct CandleTime) {
	t := time.Unix(int64(ct.Timestamp), 0).UTC()
	year, week := t.ISOWeek()

	if !f.hasWeek {
		f.isoYear = year
		f.isoWeek = week
		f.wOpen = ct.Open
		f.wHigh = ct.High
		f.wLow = ct.Low
		f.wClose = ct.Close
		f.hasWeek = true
		return
	}

	if year != f.isoYear || week != f.isoWeek {
		// Week rolled — finalise the completed weekly bar and update EMA.
		f.ema.Update(Candle{
			Open:  f.wOpen,
			High:  f.wHigh,
			Low:   f.wLow,
			Close: f.wClose,
		})
		// Start fresh accumulation for the new week.
		f.isoYear = year
		f.isoWeek = week
		f.wOpen = ct.Open
		f.wHigh = ct.High
		f.wLow = ct.Low
		f.wClose = ct.Close
	} else {
		if ct.High > f.wHigh {
			f.wHigh = ct.High
		}
		if ct.Low < f.wLow {
			f.wLow = ct.Low
		}
		f.wClose = ct.Close
	}
}

// Trending always returns true; direction is enforced via AllowSide.
func (f *WeeklyEMAFilter) Trending() bool { return true }

func (f *WeeklyEMAFilter) AllowSide(side Side) bool {
	if !f.ema.Ready() {
		return true
	}
	closePrice := float64(f.wClose) / f.scale
	emaVal := f.ema.Float64()
	switch side {
	case Long:
		return closePrice > emaVal
	case Short:
		return closePrice < emaVal
	default:
		return true
	}
}

// EMAValue exposes the current EMA value for debugging.
func (f *WeeklyEMAFilter) EMAValue() float64 { return f.ema.Float64() }
