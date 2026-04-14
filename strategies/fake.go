package strategies

import (
	"context"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// NoopStrategy does nothing.
type Fake struct {
	StrategyConfig
	CandleCount int

	candles []*market.CandleTime
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

func (f *Fake) Update(ctx context.Context, c *market.CandleTime, positions *types.Positions) *Plan {
	f.candles = append(f.candles, c)
	plan := &Plan{
		Reason: "hold",
	}

	if len(f.candles) < f.CandleCount {
		return plan
	}

	openTrades := positions.Len()
	if f.highest < c.High {
		f.highest = c.High
		if openTrades > 0 {
			return plan
		}
		inst := market.GetInstrument(f.Instrument)
		stop := inst.SubPips(c.Close, types.PipsFromFloat(10))
		op := types.NewOpenRequest(f.Instrument, c, types.Long, stop, types.Price(0), "higher highs")
		plan.Opens = append(plan.Opens, op)
	}

	if f.lowest == 0 || f.lowest > c.Low {
		f.lowest = c.Low
		if openTrades == 0 {
			return plan
		}
		cl := &types.CloseRequest{
			Request: types.Request{
				Reason: "lower low",
			},
		}
		plan.Closes = append(plan.Closes, cl)
	}

	return plan
}
