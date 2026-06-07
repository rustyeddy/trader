// Package donchianv4 is Donchian breakout v4: adds an ADX directional-strength
// gate on top of the v2 consecutive-close confirmation filter.
//
// Entry is only emitted when the ADX indicator is ready and reports:
//   - ADX value ≥ adx_threshold (trend is strong enough), AND
//   - For long entries:  +DI > -DI (buying pressure dominates)
//   - For short entries: -DI > +DI (selling pressure dominates)
//
// When ADX is still warming up, the gate is bypassed (same behaviour as v2).
// When ADX blocks an entry the pending streak is preserved; entry fires on the
// next bar where ADX conditions are satisfied without needing a fresh breakout.
//
// Registers under "donchian-v4" and "donchian-breakout-v4".
package donchianv4

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.RegisterStrategy(build, "donchian-v4", "donchian-breakout-v4")
}

// Breakout is the v4 Donchian strategy.
type Breakout struct {
	period        int
	closeStrength float64
	confirmBars   int
	adxThreshold  float64

	// Rolling channel buffer (completed bars only).
	highs []trader.Price
	lows  []trader.Price
	pos   int
	count int

	// Consecutive-close confirmation state (from v2).
	pendingSide  trader.Side
	pendingCount int
	pendingLevel trader.Price

	// ADX directional-strength indicator.
	adx *trader.ADX

	name string
}

// Config holds constructor parameters.
type Config struct {
	trader.StrategyBaseConfig

	Period        int
	CloseStrength float64
	ConfirmBars   int
	ADXPeriod     int
	ADXThreshold  float64
}

func New(cfg Config) (*Breakout, error) {
	if cfg.Period <= 1 {
		return nil, fmt.Errorf("donchianv4: period must be > 1")
	}
	if cfg.CloseStrength < 0.5 || cfg.CloseStrength > 1.0 {
		return nil, fmt.Errorf("donchianv4: close_strength must be in [0.5, 1.0]")
	}
	cb := cfg.ConfirmBars
	if cb < 1 {
		cb = 2
	}
	ap := cfg.ADXPeriod
	if ap <= 0 {
		ap = 14
	}
	at := cfg.ADXThreshold
	if at <= 0 {
		at = 25.0
	}
	adx, err := trader.NewADX(ap, trader.PriceScale)
	if err != nil {
		return nil, err
	}
	return &Breakout{
		period:        cfg.Period,
		closeStrength: cfg.CloseStrength,
		confirmBars:   cb,
		adxThreshold:  at,
		highs:         make([]trader.Price, cfg.Period),
		lows:          make([]trader.Price, cfg.Period),
		adx:           adx,
		name: fmt.Sprintf("DONCHIAN-V4(%d,cs=%.2f,cb=%d,adx=%d/%.1f)",
			cfg.Period, cfg.CloseStrength, cb, ap, at),
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
	d.adx.Reset()
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

// advanceBar pushes the candle into both the Donchian buffer and the ADX indicator.
func (d *Breakout) advanceBar(c trader.Candle) {
	d.highs[d.pos] = c.High
	d.lows[d.pos] = c.Low
	d.pos = (d.pos + 1) % d.period
	if d.count < d.period {
		d.count++
	}
	d.adx.Update(c)
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

// adxGatePass returns true when the ADX gate should allow an entry on `side`.
// Always passes when ADX is not yet ready (warmup bypass).
func (d *Breakout) adxGatePass(side trader.Side) bool {
	if !d.adx.Ready() {
		return true
	}
	if d.adx.Float64() < d.adxThreshold {
		return false
	}
	if side == trader.Long {
		return d.adx.PlusDI() > d.adx.MinusDI()
	}
	return d.adx.MinusDI() > d.adx.PlusDI()
}

func (d *Breakout) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	_ = ctx
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}

	if !d.Ready() {
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "warming up"}
	}

	hi, lo := d.channelHighLow()

	// Determine breakout direction. During an active streak compare against
	// pendingLevel (channel level at streak start) to avoid contamination.
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
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "no breakout"}
	}

	if side != d.pendingSide {
		if !closeStrengthOK(ct.Candle, side, d.closeStrength) {
			d.pendingSide = 0
			d.pendingCount = 0
			d.pendingLevel = 0
			d.advanceBar(ct.Candle)
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
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{
			Reason: fmt.Sprintf("confirming break (%d/%d)", d.pendingCount, d.confirmBars),
		}
	}

	// Confirmed — check ADX gate before emitting.
	if !d.adxGatePass(side) {
		d.advanceBar(ct.Candle)
		return &trader.StrategyPlan{
			Reason: fmt.Sprintf("adx-filtered(adx=%.1f,+DI=%.1f,-DI=%.1f)",
				d.adx.Float64(), d.adx.PlusDI(), d.adx.MinusDI()),
		}
	}

	// Confirmed and ADX gate passed — emit entry.
	d.pendingSide = 0
	d.pendingCount = 0
	d.pendingLevel = 0

	plan := emitOpen(ct, run, side)
	d.advanceBar(ct.Candle)
	if side == trader.Long {
		plan.Reason = "donchian-v4-breakout-up"
	} else {
		plan.Reason = "donchian-v4-breakout-down"
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
					Reason:      "donchian-v4-reverse",
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
	open := trader.NewOpenRequest(inst, ct, side, 0, 0, "donchian-v4-breakout")
	plan.Opens = append(plan.Opens, open)
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
	adxPeriod, ok, err := trader.GetInt32Param(params, "adx_period")
	if err != nil {
		return nil, err
	}
	if !ok {
		adxPeriod = 14
	}
	adxThreshold, ok, err := trader.GetFloat64Param(params, "adx_threshold")
	if err != nil {
		return nil, err
	}
	if !ok {
		adxThreshold = 25.0
	}
	return New(Config{
		Period:        int(period),
		CloseStrength: closeStrength,
		ConfirmBars:   int(confirmBars),
		ADXPeriod:     int(adxPeriod),
		ADXThreshold:  adxThreshold,
	})
}
