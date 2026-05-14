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

func (f *Fake) Update(ctx context.Context, c *CandleTime, run *Backtest) *StrategyPlan {
	f.candles = append(f.candles, c)
	plan := &StrategyPlan{
		Reason: "hold",
	}

	if len(f.candles) < f.CandleCount {
		return plan
	}

	openTrades := run.Lots.Len()
	if f.highest < c.High {
		f.highest = c.High
		if openTrades > 0 {
			return plan
		}
		inst := GetInstrument(run.Instrument)
		if inst == nil {
			return nil
		}
		stop := inst.SubPips(c.Close, pipsFromFloat(10))
		op := newOpenRequest(run.Instrument, c, Long, stop, Price(0), "higher highs")
		plan.Opens = append(plan.Opens, op)
	}

	if f.lowest == 0 || f.lowest > c.Low {
		f.lowest = c.Low
		if openTrades == 0 {
			return plan
		}

		submittedClose := false
		run.Lots.Range(func(lot *Lot) error {
			if lot.State != LotOpen {
				return nil
			}

			// Are there lots that need to be closed?
			if (lot.Side == Long && c.Close <= lot.Stop) ||
				(lot.Side == Short && c.Close >= lot.Stop) {
				cl := &closeRequest{
					Request: Request{
						TradeCommon: lot.TradeCommon,
						Reason:      "CloseStop",
						Candle:      c.Candle,
						RequestType: RequestClose,
						Price:       c.Close,
						Timestamp:   c.Timestamp,
					},
					CloseCause: CloseStopLoss,
					Lot:        lot,
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
