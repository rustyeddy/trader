package strategies

import (
	"context"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
)

// NoopStrategy does nothing.
type Fake struct {
	StrategyConfig
	CandleCount int

	openPosition bool

	candles []*market.Candle
	highest types.Price
	lowest  types.Price
}

func (f *Fake) Name() string {
	return "Fake"
}

func (f *Fake) Reset() {
	clear(f.candles)
	f.highest = 0.0
	f.lowest = 0.0
}

func (f *Fake) Ready() bool {
	return f.CandleCount == len(f.candles)
}

func (f *Fake) Reason() string {
	return "No-op"
}

func (f *Fake) Update(ctx context.Context, c *market.Candle) *Plan {
	f.candles = append(f.candles, c)
	plan := &Plan{
		Reason: "hold",
	}

	// fmt.Println(c.String())

	if len(f.candles) < f.CandleCount {
		if f.highest < c.High {
			f.highest = c.High
		}
		if f.lowest == 0 || f.lowest > c.Low {
			f.lowest = c.Low
		}
		return plan
	}
	plan.Reason = "hold"

	if f.lowest > c.Low {
		f.highest = c.High

		if !f.openPosition {
			op := &portfolio.OpenRequest{
				Instrument: f.Instrument,
				Price:      c.Close,
				Side:       types.Long,
				Stop:       f.Instrument.SubPips(c.Close, types.Pips(10)),
				Count:      len(f.candles),
			}
			plan.Opens = append(plan.Opens, op)
			plan.Reason = "higher high"
			f.openPosition = true
		}
	}

	if f.highest < c.High {
		// Before closing we must make sure we have an open trade
		f.lowest = c.Low
		if f.openPosition {
			cl := &portfolio.CloseRequest{
				Instrument: f.Instrument,
				Count:      len(f.candles),
				Price:      c.Close,
			}
			plan.Closes = append(plan.Closes, cl)
			plan.Reason = "lower low"
			f.openPosition = false
		}
	}

	return plan
}
