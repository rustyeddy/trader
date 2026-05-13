package trader

import "context"

// LifecycleTestStrategy is a deterministic canned lifecycle strategy.
//
// It exists to regression-test:
// config -> candles -> strategy -> Trader -> Broker -> Account -> Trades -> Result
//
// Behavior:
//
//	bar 1: open long
//	bar 3: close first open position
//
// Use a canned EURUSD dataset where:
//
//	bar 1 close = 1.10000
//	bar 3 close = 1.11000
//
// With Units = 1000, expected P/L is $10.00.
type LifecycleTestStrategy struct {
	bar        int
	opened     bool
	closed     bool
	Units      Units
	StopPips   float64
	TakeProfit Price
}

func (s *LifecycleTestStrategy) Name() string {
	return "lifecycle-test"
}

func (s *LifecycleTestStrategy) Reset() {
	s.bar = 0
	s.opened = false
	s.closed = false
}

func (s *LifecycleTestStrategy) Ready() bool {
	return true
}

func (s *LifecycleTestStrategy) Update(ctx context.Context, c *CandleTime, run *Backtest) *StrategyPlan {
	_ = ctx

	plan := &StrategyPlan{Reason: "hold"}
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

	// Bar 1: open long.
	if s.bar == 1 && !s.opened {
		inst := GetInstrument(run.Instrument)
		if inst == nil {
			plan.Reason = "lifecycle-test-missing-instrument"
			return plan
		}

		stop := inst.SubPips(c.Close, pipsFromFloat(s.StopPips))
		op := newOpenRequest(run.Instrument, c, Long, stop, 0, "lifecycle-test-open-long")
		op.Units = s.Units

		plan.Opens = append(plan.Opens, op)
		plan.Reason = "lifecycle-test-open-long"

		s.opened = true
		return plan
	}

	// Bar 3: close the first open position.
	if s.bar == 3 && s.opened && !s.closed {
		if run.Positions == nil || run.Positions.Len() == 0 {
			plan.Reason = "lifecycle-test-no-position-to-close"
			return plan
		}

		submitted := false
		_ = run.Positions.Range(func(pos *Position) error {
			if submitted {
				return nil
			}
			if pos == nil || pos.State != PositionOpen {
				return nil
			}

			cl := &closeRequest{
				Request: Request{
					TradeCommon: pos.TradeCommon,
					RequestType: RequestClose,
					Price:       c.Close,
					Timestamp:   c.Timestamp,
					Candle:      c.Candle,
					Reason:      "lifecycle-test-close-long",
				},
				Position:   pos,
				CloseCause: CloseManual,
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
