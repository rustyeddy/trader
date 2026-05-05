package trader

import "context"

// Fake02 is a deterministic lifecycle/accounting test strategy.
//
// Behavior:
// - while flat, wait WaitBars and then open
// - alternate long/short each cycle
// - hold for HoldBars
// - close at market
// - wait WaitBars again, then repeat
//
// This is intentionally simple and predictable so it can be used to
// exercise trade lifecycle and accounting logic repeatedly.
type Fake02 struct {
	Instrument string
	WaitBars   int
	HoldBars   int
	StopPips   float64

	bar        int
	nextOpenAt int
	openedAt   int
	longNext   bool
}

func (f *Fake02) Name() string {
	return "Fake02"
}

func (f *Fake02) Reset() {
	f.bar = 0
	f.nextOpenAt = 0
	f.openedAt = 0
	f.longNext = false
}

func (f *Fake02) Ready() bool {
	return true
}

func (f *Fake02) Update(ctx context.Context, c *CandleTime, run *BacktestRun) *StrategyPlan {
	_ = ctx

	plan := &StrategyPlan{
		Reason: "hold",
	}

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

	// If something is open, close it after HoldBars.
	if run.Positions != nil && run.Positions.Len() > 0 {
		if (f.bar - f.openedAt) >= f.HoldBars {
			submittedClose := false
			run.Positions.Range(func(pos *Position) error {
				if pos.State != PositionOpen {
					return nil
				}
				cl := &closeRequest{
					Request: Request{
						TradeCommon: pos.TradeCommon,
						Reason:      "fake-02-close",
						Candle:      c.Candle,
						RequestType: RequestClose,
						Price:       c.Close,
						Timestamp:   c.Timestamp,
					},
					Position:   pos,
					CloseCause: CloseManual,
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

	// Flat: wait until the next scheduled open.
	if f.bar < f.nextOpenAt {
		return plan
	}

	side := Long
	if !f.longNext {
		side = Short
	}

	inst := GetInstrument(f.Instrument)
	if inst == nil {
		plan.Reason = "fake-02-missing-instrument"
		return plan
	}

	var stop Price
	if side == Long {
		stop = inst.SubPips(c.Close, pipsFromFloat(f.StopPips))
	} else {
		stop = inst.AddPips(c.Close, pipsFromFloat(f.StopPips))
	}

	op := newOpenRequest(f.Instrument, c, side, stop, Price(0), "fake-02-open")
	plan.Opens = append(plan.Opens, op)
	plan.Reason = "fake-02-open"

	f.openedAt = f.bar
	return plan
}
