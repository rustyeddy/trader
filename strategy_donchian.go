package trader

import (
	"context"
	"fmt"
)

// DonchianBreakout enters when the close breaks above the N-bar high (long)
// or below the N-bar low (short), with a close-strength confirmation filter
// to reject weak/wick breakouts.
//
// The N-bar window is the COMPLETED bars preceding the current one — the
// current bar's high/low does not contaminate the channel.
//
// No internal stop is set; pair with an ExitStrategy (e.g. ChandelierExit)
// to manage trailing stops.
type DonchianBreakout struct {
	period        int
	closeStrength float64

	// Circular buffer of completed-bar highs and lows.
	highs []Price
	lows  []Price
	pos   int
	count int

	name string
}

type DonchianBreakoutConfig struct {
	StrategyBaseConfig

	Period        int     // N-bar lookback (e.g. 20)
	CloseStrength float64 // 0.5 = no filter; 0.6 = close in upper/lower 40% of bar
}

func NewDonchianBreakout(cfg DonchianBreakoutConfig) *DonchianBreakout {
	if cfg.Period <= 1 {
		panic("DonchianBreakout: period must be > 1")
	}
	if cfg.CloseStrength < 0.5 || cfg.CloseStrength > 1.0 {
		panic("DonchianBreakout: close_strength must be in [0.5, 1.0]")
	}
	return &DonchianBreakout{
		period:        cfg.Period,
		closeStrength: cfg.CloseStrength,
		highs:         make([]Price, cfg.Period),
		lows:          make([]Price, cfg.Period),
		name:          fmt.Sprintf("DONCHIAN(%d,cs=%.2f)", cfg.Period, cfg.CloseStrength),
	}
}

func (d *DonchianBreakout) Name() string            { return d.name }
func (d *DonchianBreakout) StopDescription() string { return "" } // delegated to ExitStrategy

func (d *DonchianBreakout) Reset() {
	for i := range d.highs {
		d.highs[i] = 0
		d.lows[i] = 0
	}
	d.pos = 0
	d.count = 0
}

func (d *DonchianBreakout) Ready() bool { return d.count >= d.period }

// channelHighLow returns the highest high and lowest low across the buffer.
// Caller must ensure Ready().
func (d *DonchianBreakout) channelHighLow() (Price, Price) {
	hi := d.highs[0]
	lo := d.lows[0]
	for i := 1; i < d.period; i++ {
		if d.highs[i] > hi {
			hi = d.highs[i]
		}
		if d.lows[i] < lo {
			lo = d.lows[i]
		}
	}
	return hi, lo
}

// pushBar appends the just-closed bar to the rolling window.
func (d *DonchianBreakout) pushBar(c Candle) {
	d.highs[d.pos] = c.High
	d.lows[d.pos] = c.Low
	d.pos = (d.pos + 1) % d.period
	if d.count < d.period {
		d.count++
	}
}

// closeStrengthOK reports whether the breakout bar's close sits in the
// favorable portion of the bar for the breakout direction.
func closeStrengthOK(c Candle, side Side, threshold float64) bool {
	rng := float64(c.High - c.Low)
	if rng <= 0 {
		return false
	}
	if side == Long {
		// fraction of range up from low to close
		return float64(c.Close-c.Low)/rng >= threshold
	}
	// short: fraction of range down from high to close
	return float64(c.High-c.Close)/rng >= threshold
}

func (d *DonchianBreakout) Update(ctx context.Context, ct *CandleTime, run *Backtest) *StrategyPlan {
	_ = ctx
	if ct == nil {
		return &DefaultStrategyPlan
	}

	if !d.Ready() {
		d.pushBar(ct.Candle)
		return &StrategyPlan{Reason: "warming up"}
	}

	hi, lo := d.channelHighLow()

	var side Side
	switch {
	case ct.Close > hi:
		side = Long
	case ct.Close < lo:
		side = Short
	default:
		d.pushBar(ct.Candle)
		return &StrategyPlan{Reason: "no breakout"}
	}

	if !closeStrengthOK(ct.Candle, side, d.closeStrength) {
		d.pushBar(ct.Candle)
		return &StrategyPlan{Reason: "weak close"}
	}

	plan := donchianEmitOpen(ct, run, side)
	d.pushBar(ct.Candle)
	if side == Long {
		plan.Reason = "donchian-breakout-up"
	} else {
		plan.Reason = "donchian-breakout-down"
	}
	return plan
}

// donchianEmitOpen closes any opposite open lots, then opens a new position.
// No internal stop is set — the ExitStrategy supplies one via InitialStop.
func donchianEmitOpen(ct *CandleTime, run *Backtest, side Side) *StrategyPlan {
	plan := &StrategyPlan{}

	if run != nil && run.Lots != nil {
		_ = run.Lots.Range(func(lot *Lot) error {
			if lot.State != LotOpen || lot.Side == side {
				return nil
			}
			plan.Closes = append(plan.Closes, &closeRequest{
				Request: Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "donchian-reverse",
					Candle:      ct.Candle,
					RequestType: RequestClose,
					Price:       ct.Close,
					Timestamp:   ct.Timestamp,
				},
				Lot:        lot,
				CloseCause: CloseManual,
			})
			return nil
		})
	}

	inst := ""
	if run != nil {
		inst = run.Instrument
	}
	plan.Opens = append(plan.Opens, newOpenRequest(inst, ct, side, 0, 0, "donchian-breakout"))
	return plan
}
