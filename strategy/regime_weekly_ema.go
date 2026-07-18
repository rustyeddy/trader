package strategy

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// WeeklyEMAFilter is a directional regime filter that aggregates sub-daily bars
// into ISO weekly bars and runs an EMA(period) over weekly closes.
//
// Trending() always returns true — this is a direction-only filter.
// AllowSide(Long)  returns true when the current week's in-progress close is
// above the EMA computed from completed weekly closes. AllowSide(Short)
// returns true when that in-progress close is below the EMA, so directional
// permission can change within a week as the partial weekly close moves.
//
// During warmup (EMA not yet ready) AllowSide returns true as a defensive
// contract so no entries are suppressed before enough weekly data has
// accumulated, although the main callers already gate on Ready() before
// consulting directional permission.
//
// Registered in the factory as "weekly-ema".
type WeeklyEMAFilter struct {
	ema    *indicator.EMA
	period int

	// Weekly bar accumulation.
	isoYear int
	isoWeek int
	wOpen   types.Price
	wHigh   types.Price
	wLow    types.Price
	wClose  types.Price
	hasWeek bool
}

func NewWeeklyEMAFilter(period int, scale types.Scale6) (*WeeklyEMAFilter, error) {
	ema, err := indicator.NewEMA(period, scale)
	if err != nil {
		return nil, err
	}
	return &WeeklyEMAFilter{
		ema:    ema,
		period: period,
	}, nil
}

func (f *WeeklyEMAFilter) Name() string {
	return fmt.Sprintf("WeeklyEMA(%d)", f.period)
}

func (f *WeeklyEMAFilter) Ready() bool { return f.ema.Ready() }

func (f *WeeklyEMAFilter) Tick(ct market.Candle) {
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
		f.ema.Update(market.Candle{
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

func (f *WeeklyEMAFilter) AllowSide(side types.Side) bool {
	if !f.ema.Ready() {
		return true
	}
	closePrice := types.PriceSum(f.wClose)
	emaVal := f.ema.PriceSum()
	switch side {
	case types.Long:
		return closePrice > emaVal
	case types.Short:
		return closePrice < emaVal
	default:
		return true
	}
}

// EMA exposes the current weekly EMA value for debugging.
func (f *WeeklyEMAFilter) EMA() float64 { return f.ema.Float64() }

// EMAValue exposes the current EMA value for debugging.
func (f *WeeklyEMAFilter) EMAValue() float64 { return f.EMA() }
