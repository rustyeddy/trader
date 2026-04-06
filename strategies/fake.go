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

func (f *Fake) Update(ctx context.Context, c *market.Candle, positions portfolio.Positions) *Plan {
	f.candles = append(f.candles, c)
	plan := &Plan{
		Reason: "hold",
	}

	inst := market.GetInstrument(f.Instrument)
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

	var posid string
	for _, pos := range positions.Positions() {
		if pos.Common.Instrument.Name == inst.Name {
			posid = pos.ID
			break
		}
	}

	if f.lowest > c.Low {
		f.highest = c.High

		if posid == "" {
			op := &portfolio.OpenRequest{
				ID: types.NewULID(),
				Common: portfolio.CommonPortfolio{
					Side:       types.Long,
					Stop:       inst.SubPips(c.Close, types.PipsFromFloat(10)),
					Instrument: inst,
					Reason:     "higher high",
				},
				Price: c.Close,
			}
			plan.Opens = append(plan.Opens, op)
			plan.Reason = "higher high"
		}
	}

	if f.highest < c.High {
		// Before closing we must make sure we have an open trade
		f.lowest = c.Low
		if posid != "" {
			cl := &portfolio.CloseRequest{
				ID: types.NewULID(),
			}
			plan.Closes = append(plan.Closes, cl)
			plan.Reason = "lower low"
		}
	}

	return plan
}
