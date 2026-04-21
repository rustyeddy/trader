package trader

import (
	"context"
)

// NoopStrategy does nothing.
type Fake struct {
	StrategyBaseConfig
	CandleCount int

	candles []*candleTime
	highest Price
	lowest  Price
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

func (f *Fake) Update(ctx context.Context, c *candleTime, positions *Positions) *StrategyPlan {
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
		inst := GetInstrument(f.Instrument)
		stop := inst.SubPips(c.Close, pipsFromFloat(10))
		op := newOpenRequest(f.Instrument, c, Long, stop, Price(0), "higher highs")
		plan.Opens = append(plan.Opens, op)
	}

	if f.lowest == 0 || f.lowest > c.Low {
		f.lowest = c.Low
		if openTrades == 0 {
			return plan
		}

		submittedClose := false
		positions.Range(func(pos *Position) error {
			if pos.State != PositionOpen {
				return nil
			}

			// Are there positions that need to be closed?
			if (pos.Side == Long && c.Close <= pos.Stop) ||
				(pos.Side == Short && c.Close >= pos.Stop) {
				cl := &closeRequest{
					Request: Request{
						TradeCommon: pos.TradeCommon,
						Reason:      "CloseStop",
						Candle:      c.Candle,
						RequestType: RequestClose,
						Price:       c.Close,
						Timestamp:   c.Timestamp,
					},
					CloseCause: CloseStopLoss,
					Position:   pos,
				}
				plan.Closes = append(plan.Closes, cl)
				submittedClose = true
			}
			return nil
		})
		if !submittedClose {
			return plan
		}
	}

	return plan
}
