// Package lifecycle is a deterministic canned strategy used to regression-test
// the full config→candles→strategy→Trader→Broker→Account→Trades→Result
// pipeline. Registers under "lifecycle-test".
package lifecycle

import (
	"context"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(build, "lifecycle-test")
}

// Strategy opens long on bar 1 and closes on bar 3. With a canned dataset
// where bar 1 close = 1.10000 and bar 3 close = 1.11000 and Units = 1000,
// expected P/L is $10.00.
type Strategy struct {
	bar    int
	opened bool
	closed bool

	Units      market.Units
	StopPips   float64
	TakeProfit market.Price
}

func (s *Strategy) Name() string            { return "lifecycle-test" }
func (s *Strategy) StopDescription() string { return "" }

func (s *Strategy) Reset() {
	s.bar = 0
	s.opened = false
	s.closed = false
}

func (s *Strategy) Ready() bool { return true }

func (s *Strategy) Update(ctx context.Context, c *market.CandleTime, run strategy.StrategyContext) *strategy.StrategyPlan {
	_ = ctx

	plan := &strategy.StrategyPlan{Reason: "hold"}
	if c == nil || run == nil {
		return plan
	}

	s.bar++

	if s.StopPips <= 0 {
		s.StopPips = 20
	}
	if s.Units <= 0 {
		s.Units = 1000
	}

	if s.bar == 1 && !s.opened {
		inst := market.GetInstrument(run.Instrument())
		if inst == nil {
			plan.Reason = "lifecycle-test-missing-instrument"
			return plan
		}

		stop := inst.SubPips(c.Close, market.PipsFromFloat(s.StopPips))
		op := execution.NewOpenRequest(run.Instrument(), c, market.Long, stop, 0, "lifecycle-test-open-long")
		op.Units = s.Units

		plan.Opens = append(plan.Opens, op)
		plan.Reason = "lifecycle-test-open-long"

		s.opened = true
		return plan
	}

	if s.bar == 3 && s.opened && !s.closed {
		if run == nil || run.OpenLots().Len() == 0 {
			plan.Reason = "lifecycle-test-no-position-to-close"
			return plan
		}

		submitted := false
		_ = run.OpenLots().Range(func(lot *execution.Lot) error {
			if submitted {
				return nil
			}
			if lot == nil || lot.State != execution.LotOpen {
				return nil
			}

			cl := &execution.CloseRequest{
				Request: execution.Request{
					TradeCommon: lot.TradeCommon,
					RequestType: execution.RequestClose,
					Price:       c.Close,
					Timestamp:   c.Timestamp,
					Candle:      c.Candle,
					Reason:      "lifecycle-test-close-long",
				},
				Lot:        lot,
				CloseCause: execution.CloseManual,
			}

			plan.Closes = append(plan.Closes, cl)
			plan.Reason = "lifecycle-test-close-long"

			submitted = true
			return nil
		})

		if submitted {
			s.closed = true
		}
		return plan
	}

	return plan
}

func build(params map[string]any) (strategy.Strategy, error) {
	return &Strategy{
		Units:    1000,
		StopPips: 20,
	}, nil
}
