// Package fake contains canned deterministic strategies used by trader's
// integration and lifecycle tests. Registers "fake" and "fake-02".
package fake

import (
	"context"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(buildFake, "fake")
	strategy.MustRegisterStrategy(buildFake02, "fake-02")
}

// Fake opens long on higher highs, closes on stop loss on lower lows.
type Fake struct {
	CandleCount int

	candles []*trader.CandleTime
	highest trader.Price
	lowest  trader.Price
}

func (f *Fake) Name() string            { return "Fake" }
func (f *Fake) StopDescription() string { return "" }

func (f *Fake) Reset() {
	clear(f.candles)
	f.highest = 0
	f.lowest = 0
}

func (f *Fake) Ready() bool {
	return f.CandleCount == len(f.candles)
}

func (f *Fake) Update(ctx context.Context, c *trader.CandleTime, run strategy.StrategyContext) *strategy.StrategyPlan {
	f.candles = append(f.candles, c)
	plan := &strategy.StrategyPlan{Reason: "hold"}

	if len(f.candles) < f.CandleCount {
		return plan
	}

	openTrades := run.OpenLots().Len()
	if f.highest < c.High {
		f.highest = c.High
		if openTrades > 0 {
			return plan
		}
		inst := trader.GetInstrument(run.Instrument())
		if inst == nil {
			return nil
		}
		stop := inst.SubPips(c.Close, trader.PipsFromFloat(10))
		op := execution.NewOpenRequest(run.Instrument(), c, trader.Long, stop, trader.Price(0), "higher highs")
		plan.Opens = append(plan.Opens, op)
	}

	if f.lowest == 0 || f.lowest > c.Low {
		f.lowest = c.Low
		if openTrades == 0 {
			return plan
		}

		submittedClose := false
		run.OpenLots().Range(func(lot *execution.Lot) error {
			if lot.State != execution.LotOpen {
				return nil
			}

			if (lot.Side == trader.Long && c.Close <= lot.Stop) ||
				(lot.Side == trader.Short && c.Close >= lot.Stop) {
				cl := &execution.CloseRequest{
					Request: execution.Request{
						TradeCommon: lot.TradeCommon,
						Reason:      "CloseStop",
						Candle:      c.Candle,
						RequestType: execution.RequestClose,
						Price:       c.Close,
						Timestamp:   c.Timestamp,
					},
					CloseCause: execution.CloseStopLoss,
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

// Fake02 is a deterministic lifecycle/accounting test strategy.
type Fake02 struct {
	WaitBars int
	HoldBars int
	StopPips float64

	bar        int
	nextOpenAt int
	openedAt   int
	longNext   bool
}

func (f *Fake02) Name() string            { return "Fake02" }
func (f *Fake02) StopDescription() string { return "" }

func (f *Fake02) Reset() {
	f.bar = 0
	f.nextOpenAt = 0
	f.openedAt = 0
	f.longNext = false
}

func (f *Fake02) Ready() bool { return true }

func (f *Fake02) Update(ctx context.Context, c *trader.CandleTime, run strategy.StrategyContext) *strategy.StrategyPlan {
	_ = ctx

	plan := &strategy.StrategyPlan{Reason: "hold"}
	if c == nil {
		return plan
	}

	if f.WaitBars <= 0 {
		f.WaitBars = 8
	}
	if f.HoldBars <= 0 {
		f.HoldBars = 6
	}
	if f.StopPips <= 0 {
		f.StopPips = 20
	}
	if f.nextOpenAt == 0 {
		f.nextOpenAt = 1
		f.longNext = true
	}

	f.bar++

	if run != nil && run.OpenLots().Len() > 0 {
		if (f.bar - f.openedAt) >= f.HoldBars {
			submittedClose := false
			run.OpenLots().Range(func(lot *execution.Lot) error {
				if lot.State != execution.LotOpen {
					return nil
				}
				cl := &execution.CloseRequest{
					Request: execution.Request{
						TradeCommon: lot.TradeCommon,
						Reason:      "fake-02-close",
						Candle:      c.Candle,
						RequestType: execution.RequestClose,
						Price:       c.Close,
						Timestamp:   c.Timestamp,
					},
					Lot:        lot,
					CloseCause: execution.CloseManual,
				}
				plan.Closes = append(plan.Closes, cl)
				submittedClose = true
				return nil
			})
			if !submittedClose {
				return plan
			}

			f.nextOpenAt = f.bar + f.WaitBars
			f.longNext = !f.longNext
			plan.Reason = "fake-02-close"
			return plan
		}
		return plan
	}

	if f.bar < f.nextOpenAt {
		return plan
	}

	side := trader.Long
	if !f.longNext {
		side = trader.Short
	}

	inst := trader.GetInstrument(run.Instrument())
	if inst == nil {
		plan.Reason = "fake-02-missing-instrument"
		return plan
	}

	var stop trader.Price
	if side == trader.Long {
		stop = inst.SubPips(c.Close, trader.PipsFromFloat(f.StopPips))
	} else {
		stop = inst.AddPips(c.Close, trader.PipsFromFloat(f.StopPips))
	}

	op := execution.NewOpenRequest(run.Instrument(), c, side, stop, trader.Price(0), "fake-02-open")
	plan.Opens = append(plan.Opens, op)
	plan.Reason = "fake-02-open"

	f.openedAt = f.bar
	return plan
}

func buildFake(params map[string]any) (strategy.Strategy, error) {
	return &Fake{CandleCount: 10}, nil
}

func buildFake02(params map[string]any) (strategy.Strategy, error) {
	return &Fake02{
		WaitBars: 8,
		HoldBars: 6,
	}, nil
}
