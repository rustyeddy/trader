package trader

import (
	"context"

	"github.com/rustyeddy/trader/types"
)

// NoopStrategy does nothing.
type Fake struct {
	StrategyBaseConfig
	CandleCount int

	candles []*types.CandleTime
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

func (f *Fake) Update(ctx context.Context, c *types.CandleTime, positions *types.Positions) *StrategyPlan {
	f.candles = append(f.candles, c)
	plan := &StrategyPlan{
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
		inst := types.GetInstrument(f.Instrument)
		stop := inst.SubPips(c.Close, types.PipsFromFloat(10))
		op := types.NewOpenRequest(f.Instrument, c, types.Long, stop, types.Price(0), "higher highs")
		plan.Opens = append(plan.Opens, op)
	}

	if f.lowest == 0 || f.lowest > c.Low {
		f.lowest = c.Low
		if openTrades == 0 {
			return plan
		}

		positions.Range(func(pos *types.Position) error {

			// Are there positions that need to be closed?
			if pos.Side == types.Long && c.Close <= pos.Stop {
				cl := &types.CloseRequest{
					Request: types.Request{
						TradeCommon: pos.TradeCommon,
						Reason:      "lower low",
						Candle:      c.Candle,
						RequestType: types.RequestClose,
						Price:       c.Close,
						Timestamp:   c.Timestamp,
					},
					CloseCause: types.CloseStopLoss,
					Position:   pos,
				}
				plan.Closes = append(plan.Closes, cl)
			}

			// Is there an open signal to be become a request
			if pos.Side == types.Short && c.Close >= pos.Stop {
				cl := &types.CloseRequest{
					Request: types.Request{
						TradeCommon: pos.TradeCommon,
						Reason:      "close stop",
						Candle:      c.Candle,
						RequestType: types.RequestClose,
						Price:       c.Close,
						Timestamp:   c.Timestamp,
					},
					CloseCause: types.CloseStopLoss,
					Position:   pos,
				}
				plan.Closes = append(plan.Closes, cl)
			}
			return nil
		})
	}

	return plan
}
