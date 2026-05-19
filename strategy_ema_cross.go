package trader

import (
	"context"
	"fmt"
)

type emaCrossCore struct {
	fast *EMA
	slow *EMA
	atr  *ATR // nil when ATR stop not configured

	name          string
	prevRel       int
	minSpread     float64
	scale         Scale6
	stopPips      Pips    // fixed-pip fallback when atr is nil
	atrMultiplier float64 // multiplier applied to ATR value
}

// EMACross generates signals when a fast EMA crosses a slow EMA.
type EMACross struct {
	core emaCrossCore
}

type EMACrossConfig struct {
	StrategyBaseConfig

	FastPeriod    int
	SlowPeriod    int
	Scale         Scale6
	MinSpread     float64
	StopPips      Pips    // used when ATRPeriod == 0
	ATRPeriod     int     // 0 = disabled; use StopPips instead
	ATRMultiplier float64 // default 1.5 when ATRPeriod > 0
}

func NewEMACross(cfg EMACrossConfig) *EMACross {
	if cfg.FastPeriod <= 0 || cfg.SlowPeriod <= 0 {
		panic("EMACross periods must be > 0")
	}
	if cfg.FastPeriod >= cfg.SlowPeriod {
		panic("EMACross requires FastPeriod < SlowPeriod")
	}
	if cfg.Scale <= 0 {
		panic("EMACross requires Scale > 0")
	}

	mult := cfg.ATRMultiplier
	if cfg.ATRPeriod > 0 && mult <= 0 {
		mult = 1.5
	}

	var atr *ATR
	if cfg.ATRPeriod > 0 {
		atr = NewATR(cfg.ATRPeriod, cfg.Scale)
	}

	return &EMACross{
		core: emaCrossCore{
			fast:          NewEMA(cfg.FastPeriod, cfg.Scale),
			slow:          NewEMA(cfg.SlowPeriod, cfg.Scale),
			atr:           atr,
			prevRel:       0,
			minSpread:     cfg.MinSpread,
			scale:         cfg.Scale,
			stopPips:      cfg.StopPips,
			atrMultiplier: mult,
			name:          fmt.Sprintf("EMA_CROSS(%d,%d)", cfg.FastPeriod, cfg.SlowPeriod),
		},
	}
}

func (x *EMACross) Name() string { return x.core.name }

func (x *EMACross) Reset() {
	x.core.fast.Reset()
	x.core.slow.Reset()
	if x.core.atr != nil {
		x.core.atr.Reset()
	}
	x.core.prevRel = 0
}

func (x *EMACross) Ready() bool {
	return x.core.fast.Ready() && x.core.slow.Ready()
}

func (x *EMACross) Update(ctx context.Context, ct *CandleTime, run *Backtest) *StrategyPlan {
	_ = ctx
	if ct == nil {
		return &DefaultStrategyPlan
	}
	c := ct.Candle
	x.core.fast.Update(c)
	x.core.slow.Update(c)
	if x.core.atr != nil {
		x.core.atr.Update(c)
	}

	if !x.Ready() {
		return &StrategyPlan{Reason: "warming up"}
	}

	fv := x.core.fast.Float64()
	sv := x.core.slow.Float64()
	diff := fv - sv

	if x.core.minSpread > 0 && abs(diff) < x.core.minSpread {
		return &StrategyPlan{Reason: "min-spread filter"}
	}

	rel := 0
	if diff > 0 {
		rel = +1
	} else if diff < 0 {
		rel = -1
	}

	if x.core.prevRel == 0 {
		x.core.prevRel = rel
		return &StrategyPlan{Reason: "baseline set"}
	}

	// Cross up → go long
	if x.core.prevRel == -1 && rel == +1 {
		x.core.prevRel = rel
		if x.core.atr != nil && !x.core.atr.Ready() {
			return &StrategyPlan{Reason: "warming up ATR"}
		}
		plan := emaCrossEmitOpen(&x.core, ct, run, Long)
		plan.Reason = "ema-cross-up"
		return plan
	}

	// Cross down → go short
	if x.core.prevRel == +1 && rel == -1 {
		x.core.prevRel = rel
		if x.core.atr != nil && !x.core.atr.Ready() {
			return &StrategyPlan{Reason: "warming up ATR"}
		}
		plan := emaCrossEmitOpen(&x.core, ct, run, Short)
		plan.Reason = "ema-cross-down"
		return plan
	}

	x.core.prevRel = rel
	return &StrategyPlan{Reason: "no cross"}
}

// emaCrossEmitOpen closes any opposite open lots then opens a new position in side.
func emaCrossEmitOpen(core *emaCrossCore, ct *CandleTime, run *Backtest, side Side) *StrategyPlan {
	plan := &StrategyPlan{}

	if run != nil && run.Lots != nil {
		_ = run.Lots.Range(func(lot *Lot) error {
			if lot.State != LotOpen || lot.Side == side {
				return nil
			}
			plan.Closes = append(plan.Closes, &closeRequest{
				Request: Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "ema-cross-reverse",
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

	stop := emaCrossStop(core, ct, inst, side)
	plan.Opens = append(plan.Opens, newOpenRequest(inst, ct, side, stop, 0, "ema-cross"))
	return plan
}

// emaCrossStop computes the stop price: ATR-based if configured and ready, fixed pips otherwise.
func emaCrossStop(core *emaCrossCore, ct *CandleTime, inst string, side Side) Price {
	if core.atr != nil && core.atr.Ready() {
		dist := Price(core.atr.Float64() * core.atrMultiplier * float64(core.scale))
		if side == Long {
			return ct.Close - dist
		}
		return ct.Close + dist
	}

	if core.stopPips > 0 && inst != "" {
		if instr := GetInstrument(inst); instr != nil {
			if side == Long {
				return instr.SubPips(ct.Close, core.stopPips)
			}
			return instr.AddPips(ct.Close, core.stopPips)
		}
	}

	return 0
}
