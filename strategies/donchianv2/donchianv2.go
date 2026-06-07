// Package donchianv2 is Donchian breakout v2: adds a consecutive-close
// confirmation filter (confirm_bars, default 2) on top of the v1 close-strength
// filter. An entry is only emitted after N consecutive closes beyond the channel
// level recorded when the streak began, reducing false breakouts caused by
// single-bar wicks or low-momentum pushes.
//
// Key design: once a streak is started the continuation check compares against
// the original channel level (pendingLevel), not the live channel. This prevents
// the breakout bar's own high/low from raising the bar for bar 2 and silently
// killing every streak on the very next candle.
//
// Registers under "donchian-v2" and "donchian-breakout-v2".
package donchianv2

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.RegisterStrategy(build, "donchian-v2", "donchian-breakout-v2")
}

// Breakout is the v2 Donchian strategy.
type Breakout struct {
	period        int
	closeStrength float64
	confirmBars   int // consecutive closes required before entry (default 2)

	// Rolling channel buffer (completed bars only).
	highs []trader.Price
	lows  []trader.Price
	pos   int
	count int

	// Consecutive-close confirmation state.
	pendingSide  trader.Side  // direction of current streak (0 = none)
	pendingCount int          // bars confirmed in current streak
	pendingLevel trader.Price // channel level recorded at the start of the streak
	name         string
}

// Config holds constructor parameters.
type Config struct {
	trader.StrategyBaseConfig

	Period        int     // N-bar lookback (e.g. 20)
	CloseStrength float64 // 0.5 = no filter; 0.6 = close in upper/lower 40% of bar
	ConfirmBars   int     // consecutive closes beyond channel required (default 2)
}

func New(cfg Config) (*Breakout, error) {
	if cfg.Period <= 1 {
		return nil, fmt.Errorf("donchianv2: period must be > 1")
	}
	if cfg.CloseStrength < 0.5 || cfg.CloseStrength > 1.0 {
		return nil, fmt.Errorf("donchianv2: close_strength must be in [0.5, 1.0]")
	}
	cb := cfg.ConfirmBars
	if cb < 1 {
		cb = 2
	}
	return &Breakout{
		period:        cfg.Period,
		closeStrength: cfg.CloseStrength,
		confirmBars:   cb,
		highs:         make([]trader.Price, cfg.Period),
		lows:          make([]trader.Price, cfg.Period),
		name: fmt.Sprintf("DONCHIAN-V2(%d,cs=%.2f,cb=%d)",
			cfg.Period, cfg.CloseStrength, cb),
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

func (d *Breakout) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	_ = ctx
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}

	if !d.Ready() {
		d.pushBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "warming up"}
	}

	hi, lo := d.channelHighLow()

	// Determine breakout direction for this bar. When a streak is active we
	// compare against pendingLevel (the channel level when the streak began)
	// rather than the current channel — otherwise bar 1's high/low contaminates
	// the buffer and bar 2 can never continue the streak at the same price.
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
		// If streak is broken, check whether the bar reverses direction.
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
		// New streak direction — apply close-strength check on bar 1 only.
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

	// Confirmed — emit entry and reset streak.
	d.pendingSide = 0
	d.pendingCount = 0
	d.pendingLevel = 0

	plan := emitOpen(ct, run, side)
	d.pushBar(ct.Candle)
	if side == trader.Long {
		plan.Reason = "donchian-v2-breakout-up"
	} else {
		plan.Reason = "donchian-v2-breakout-down"
	}
	return plan
}

func emitOpen(ct *trader.CandleTime, run *trader.Backtest, side trader.Side) *trader.StrategyPlan {
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
					Reason:      "donchian-v2-reverse",
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
	plan.Opens = append(plan.Opens, trader.NewOpenRequest(inst, ct, side, 0, 0, "donchian-v2-breakout"))
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
	return New(Config{
		Period:        int(period),
		CloseStrength: closeStrength,
		ConfirmBars:   int(confirmBars),
	})
}
