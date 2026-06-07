// Package donchianv3 is Donchian breakout v3: adds a same-day re-entry block
// on top of the v2 consecutive-close confirmation filter.
//
// After a stop-out, no new position is opened for the rest of the calendar day
// (UTC). The stop-out is detected by noticing that a lot we opened is gone from
// the LotBook without us having manually closed it (a manual reversal close does
// not trigger the block).
//
// Registers under "donchian-v3" and "donchian-breakout-v3".
package donchianv3

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.RegisterStrategy(build, "donchian-v3", "donchian-breakout-v3")
}

// Breakout is the v3 Donchian strategy.
type Breakout struct {
	period        int
	closeStrength float64
	confirmBars   int
	sameDayBlock  bool

	// Rolling channel buffer (completed bars only).
	highs []trader.Price
	lows  []trader.Price
	pos   int
	count int

	// Consecutive-close confirmation state (from v2).
	pendingSide  trader.Side
	pendingCount int
	pendingLevel trader.Price

	// Same-day re-entry block state.
	openLotID   string // ID of the lot we most recently opened
	manualClose bool   // true if WE closed openLotID (don't block on manual close)
	blockedDay  int64  // UTC day (unix_sec/86400) when we were last stopped out

	name string
}

// Config holds constructor parameters.
type Config struct {
	trader.StrategyBaseConfig

	Period        int
	CloseStrength float64
	ConfirmBars   int
	SameDayBlock  bool // block re-entry after a stop-out for the rest of the day
}

func New(cfg Config) (*Breakout, error) {
	if cfg.Period <= 1 {
		return nil, fmt.Errorf("donchianv3: period must be > 1")
	}
	if cfg.CloseStrength < 0.5 || cfg.CloseStrength > 1.0 {
		return nil, fmt.Errorf("donchianv3: close_strength must be in [0.5, 1.0]")
	}
	cb := cfg.ConfirmBars
	if cb < 1 {
		cb = 2
	}
	return &Breakout{
		period:        cfg.Period,
		closeStrength: cfg.CloseStrength,
		confirmBars:   cb,
		sameDayBlock:  cfg.SameDayBlock,
		highs:         make([]trader.Price, cfg.Period),
		lows:          make([]trader.Price, cfg.Period),
		name: fmt.Sprintf("DONCHIAN-V3(%d,cs=%.2f,cb=%d,sdb=%v)",
			cfg.Period, cfg.CloseStrength, cb, cfg.SameDayBlock),
	}, nil
}

func (d *Breakout) Name() string            { return d.name }
func (d *Breakout) StopDescription() string { return "" }

func (d *Breakout) Reset() {
	for i := range d.highs {
		d.highs[i] = 0
		d.lows[i] = 0
	}
	d.pos = 0
	d.count = 0
	d.pendingSide = 0
	d.pendingCount = 0
	d.pendingLevel = 0
	d.openLotID = ""
	d.manualClose = false
	d.blockedDay = 0
}

func (d *Breakout) Ready() bool { return d.count >= d.period }

func (d *Breakout) channelHighLow() (trader.Price, trader.Price) {
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

func (d *Breakout) pushBar(c trader.Candle) {
	d.highs[d.pos] = c.High
	d.lows[d.pos] = c.Low
	d.pos = (d.pos + 1) % d.period
	if d.count < d.period {
		d.count++
	}
}

func closeStrengthOK(c trader.Candle, side trader.Side, threshold float64) bool {
	rng := float64(c.High - c.Low)
	if rng <= 0 {
		return false
	}
	if side == trader.Long {
		return float64(c.Close-c.Low)/rng >= threshold
	}
	return float64(c.High-c.Close)/rng >= threshold
}

// lotIsOpen returns true if the given lot ID is present and open in the book.
func lotIsOpen(run *trader.Backtest, id string) bool {
	if run == nil || run.State == nil || run.State.Lots == nil || id == "" {
		return false
	}
	found := false
	_ = run.State.Lots.Range(func(lot *trader.Lot) error {
		if lot.ID == id && lot.State == trader.LotOpen {
			found = true
		}
		return nil
	})
	return found
}

func (d *Breakout) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	_ = ctx
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}

	currentDay := int64(ct.Timestamp) / 86400

	// --- Same-day block: detect stop-outs ---
	// autoCloseExits runs before Update(), so a stopped-out lot is already gone.
	// If our tracked lot disappeared without us requesting the close, it was
	// stopped out — block new entries for the rest of today.
	if d.sameDayBlock && d.openLotID != "" {
		if !lotIsOpen(run, d.openLotID) {
			if !d.manualClose {
				d.blockedDay = currentDay
			}
			d.openLotID = ""
			d.manualClose = false
		}
	}

	if !d.Ready() {
		d.pushBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "warming up"}
	}

	// Apply the same-day block before evaluating any breakout signal.
	if d.sameDayBlock && d.blockedDay == currentDay {
		d.pushBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "same-day block"}
	}

	hi, lo := d.channelHighLow()

	// Determine breakout direction. When a streak is active, compare against
	// pendingLevel (the channel level at streak start) rather than the live
	// channel — same design as v2.
	var side trader.Side
	if d.pendingCount > 0 {
		switch d.pendingSide {
		case trader.Long:
			if ct.Close > d.pendingLevel {
				side = trader.Long
			}
		case trader.Short:
			if ct.Close < d.pendingLevel {
				side = trader.Short
			}
		}
		if side == 0 {
			switch {
			case ct.Close > hi:
				side = trader.Long
			case ct.Close < lo:
				side = trader.Short
			}
		}
	} else {
		switch {
		case ct.Close > hi:
			side = trader.Long
		case ct.Close < lo:
			side = trader.Short
		}
	}

	if side == 0 {
		d.pendingSide = 0
		d.pendingCount = 0
		d.pendingLevel = 0
		d.pushBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "no breakout"}
	}

	if side != d.pendingSide {
		if !closeStrengthOK(ct.Candle, side, d.closeStrength) {
			d.pendingSide = 0
			d.pendingCount = 0
			d.pendingLevel = 0
			d.pushBar(ct.Candle)
			return &trader.StrategyPlan{Reason: "weak close"}
		}
		d.pendingSide = side
		d.pendingCount = 1
		if side == trader.Long {
			d.pendingLevel = hi
		} else {
			d.pendingLevel = lo
		}
	} else {
		d.pendingCount++
	}

	if d.pendingCount < d.confirmBars {
		d.pushBar(ct.Candle)
		return &trader.StrategyPlan{
			Reason: fmt.Sprintf("confirming break (%d/%d)", d.pendingCount, d.confirmBars),
		}
	}

	// Confirmed — emit entry.
	d.pendingSide = 0
	d.pendingCount = 0
	d.pendingLevel = 0

	plan := emitOpen(ct, run, side, d)
	d.pushBar(ct.Candle)
	if side == trader.Long {
		plan.Reason = "donchian-v3-breakout-up"
	} else {
		plan.Reason = "donchian-v3-breakout-down"
	}
	return plan
}

func emitOpen(ct *trader.CandleTime, run *trader.Backtest, side trader.Side, d *Breakout) *trader.StrategyPlan {
	plan := &trader.StrategyPlan{}

	alreadyOpen := false
	if run != nil && run.State != nil && run.State.Lots != nil {
		_ = run.State.Lots.Range(func(lot *trader.Lot) error {
			if lot.State != trader.LotOpen {
				return nil
			}
			if lot.Side == side {
				alreadyOpen = true
				return nil
			}
			plan.Closes = append(plan.Closes, &trader.CloseRequest{
				Request: trader.Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "donchian-v3-reverse",
					Candle:      ct.Candle,
					RequestType: trader.RequestClose,
					Price:       ct.Close,
					Timestamp:   ct.Timestamp,
				},
				Lot:        lot,
				CloseCause: trader.CloseManual,
			})
			return nil
		})
	}

	if alreadyOpen {
		return plan
	}

	inst := ""
	if run != nil && run.Request != nil {
		inst = run.Request.Instrument
	}
	open := trader.NewOpenRequest(inst, ct, side, 0, 0, "donchian-v3-breakout")
	plan.Opens = append(plan.Opens, open)

	// Track the new lot and mark any closes as manual (reversal, not stop-out).
	d.openLotID = open.ID
	if len(plan.Closes) > 0 {
		d.manualClose = true
	}

	return plan
}

func build(params map[string]any) (trader.Strategy, error) {
	period, _, err := trader.GetInt32Param(params, "period")
	if err != nil {
		return nil, err
	}
	if period <= 1 {
		period = 20
	}
	closeStrength, ok, err := trader.GetFloat64Param(params, "close_strength")
	if err != nil {
		return nil, err
	}
	if !ok {
		closeStrength = 0.6
	}
	confirmBars, ok, err := trader.GetInt32Param(params, "confirm_bars")
	if err != nil {
		return nil, err
	}
	if !ok {
		confirmBars = 2
	}
	sameDayBlock, ok, err := trader.GetBoolParam(params, "same_day_block")
	if err != nil {
		return nil, err
	}
	if !ok {
		sameDayBlock = true
	}
	return New(Config{
		Period:        int(period),
		CloseStrength: closeStrength,
		ConfirmBars:   int(confirmBars),
		SameDayBlock:  sameDayBlock,
	})
}
