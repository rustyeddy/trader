// Package emacross implements the fast/slow EMA crossover strategy.
// Registers under "ema-cross".
package emacross

import (
	"context"
	"fmt"
	"math"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.RegisterStrategy(build, "ema-cross")
}

// Core holds the shared EMA-cross state used by both ema-cross and
// ema-cross-adx strategies. Exported so the ADX variant in a sibling
// package can reuse the open/stop logic.
type Core struct {
	Fast *trader.EMA
	Slow *trader.EMA
	ATR  *trader.ATR // nil when ATR stop not configured

	Name          string
	PrevRel       int
	MinSpread     trader.Price
	Scale         trader.Scale6
	StopPips      trader.Pips
	ATRMultiplier float64
}

// Cross generates signals when a fast EMA crosses a slow EMA.
type Cross struct {
	core Core
}

type Config struct {
	trader.StrategyBaseConfig

	FastPeriod    int
	SlowPeriod    int
	Scale         trader.Scale6
	MinSpread     float64
	StopPips      trader.Pips
	ATRPeriod     int
	ATRMultiplier float64
}

func New(cfg Config) (*Cross, error) {
	if cfg.FastPeriod <= 0 || cfg.SlowPeriod <= 0 {
		return nil, fmt.Errorf("emacross periods must be > 0")
	}
	if cfg.FastPeriod >= cfg.SlowPeriod {
		return nil, fmt.Errorf("emacross requires FastPeriod < SlowPeriod")
	}
	if cfg.Scale <= 0 {
		return nil, fmt.Errorf("emacross requires Scale > 0")
	}

	mult := cfg.ATRMultiplier
	if cfg.ATRPeriod > 0 && mult <= 0 {
		mult = 1.5
	}

	var atr *trader.ATR
	if cfg.ATRPeriod > 0 {
		var err error
		atr, err = trader.NewATR(cfg.ATRPeriod, cfg.Scale)
		if err != nil {
			return nil, err
		}
	}

	fast, err := trader.NewEMA(cfg.FastPeriod, cfg.Scale)
	if err != nil {
		return nil, err
	}
	slow, err := trader.NewEMA(cfg.SlowPeriod, cfg.Scale)
	if err != nil {
		return nil, err
	}

	return &Cross{
		core: Core{
			Fast:          fast,
			Slow:          slow,
			ATR:           atr,
			MinSpread:     priceFromFloat(cfg.MinSpread, cfg.Scale),
			Scale:         cfg.Scale,
			StopPips:      cfg.StopPips,
			ATRMultiplier: mult,
			Name:          fmt.Sprintf("EMA_CROSS(%d,%d)", cfg.FastPeriod, cfg.SlowPeriod),
		},
	}, nil
}

func (x *Cross) Name() string            { return x.core.Name }
func (x *Cross) StopDescription() string { return StopDesc(&x.core) }

func (x *Cross) Reset() {
	x.core.Fast.Reset()
	x.core.Slow.Reset()
	if x.core.ATR != nil {
		x.core.ATR.Reset()
	}
	x.core.PrevRel = 0
}

func (x *Cross) Ready() bool {
	return x.core.Fast.Ready() && x.core.Slow.Ready()
}

func (x *Cross) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	_ = ctx
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}
	c := ct.Candle
	x.core.Fast.Update(c)
	x.core.Slow.Update(c)
	if x.core.ATR != nil {
		x.core.ATR.Update(c)
	}

	if !x.Ready() {
		return &trader.StrategyPlan{Reason: "warming up"}
	}

	diff := x.core.Fast.PriceSum() - x.core.Slow.PriceSum()

	if x.core.MinSpread > 0 && absPriceSum(diff) < trader.PriceSum(x.core.MinSpread) {
		return &trader.StrategyPlan{Reason: "min-spread filter"}
	}

	rel := 0
	if diff > 0 {
		rel = +1
	} else if diff < 0 {
		rel = -1
	}

	if x.core.PrevRel == 0 {
		x.core.PrevRel = rel
		return &trader.StrategyPlan{Reason: "baseline set"}
	}

	if x.core.PrevRel == -1 && rel == +1 {
		x.core.PrevRel = rel
		if x.core.ATR != nil && !x.core.ATR.Ready() {
			return &trader.StrategyPlan{Reason: "warming up ATR"}
		}
		plan := EmitOpen(&x.core, ct, run, trader.Long)
		plan.Reason = "ema-cross-up"
		return plan
	}

	if x.core.PrevRel == +1 && rel == -1 {
		x.core.PrevRel = rel
		if x.core.ATR != nil && !x.core.ATR.Ready() {
			return &trader.StrategyPlan{Reason: "warming up ATR"}
		}
		plan := EmitOpen(&x.core, ct, run, trader.Short)
		plan.Reason = "ema-cross-down"
		return plan
	}

	x.core.PrevRel = rel
	return &trader.StrategyPlan{Reason: "no cross"}
}

func absPriceSum(v trader.PriceSum) trader.PriceSum {
	if v < 0 {
		return -v
	}
	return v
}

// EmitOpen closes any opposite open lots, then opens a new position in the
// given side. Exported so emacrossadx can reuse it.
func EmitOpen(c *Core, ct *trader.CandleTime, run *trader.Backtest, side trader.Side) *trader.StrategyPlan {
	plan := &trader.StrategyPlan{}

	if run != nil && run.State != nil && run.State.Lots != nil {
		_ = run.State.Lots.Range(func(lot *trader.Lot) error {
			if lot.State != trader.LotOpen || lot.Side == side {
				return nil
			}
			plan.Closes = append(plan.Closes, &trader.CloseRequest{
				Request: trader.Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "ema-cross-reverse",
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

	inst := ""
	if run != nil && run.Request != nil {
		inst = run.Request.Instrument
	}

	stop := Stop(c, ct, inst, side)
	plan.Opens = append(plan.Opens, trader.NewOpenRequest(inst, ct, side, stop, 0, "ema-cross"))
	return plan
}

// StopDesc returns a human-readable description of the stop method.
func StopDesc(c *Core) string {
	if c.ATR != nil {
		return fmt.Sprintf("ATR(%d)×%.1f", c.ATR.Period(), c.ATRMultiplier)
	}
	if c.StopPips > 0 {
		return fmt.Sprintf("%.1f pips", c.StopPips.Float64())
	}
	return ""
}

// Stop computes the stop price: ATR-based if configured and ready, fixed pips otherwise.
func Stop(c *Core, ct *trader.CandleTime, inst string, side trader.Side) trader.Price {
	if c.ATR != nil && c.ATR.Ready() {
		dist := trader.Price(c.ATR.Float64() * c.ATRMultiplier * float64(c.Scale))
		if side == trader.Long {
			return ct.Close - dist
		}
		return ct.Close + dist
	}

	if c.StopPips > 0 && inst != "" {
		if instr := trader.GetInstrument(inst); instr != nil {
			if side == trader.Long {
				return instr.SubPips(ct.Close, c.StopPips)
			}
			return instr.AddPips(ct.Close, c.StopPips)
		}
	}

	return 0
}

func priceFromFloat(v float64, scale trader.Scale6) trader.Price {
	return trader.Price(math.Round(v * float64(scale)))
}

func build(params map[string]any) (trader.Strategy, error) {
	fast, ok, err := trader.GetInt32Param(params, "fast")
	if err != nil {
		return nil, err
	}
	if !ok || fast <= 0 {
		return nil, fmt.Errorf("ema-cross: missing or invalid param %q", "fast")
	}
	slow, ok, err := trader.GetInt32Param(params, "slow")
	if err != nil {
		return nil, err
	}
	if !ok || slow <= 0 {
		return nil, fmt.Errorf("ema-cross: missing or invalid param %q", "slow")
	}
	if fast >= slow {
		return nil, fmt.Errorf("ema-cross: fast (%d) must be < slow (%d)", fast, slow)
	}
	stopPips, _, err := trader.GetFloat64Param(params, "stop_pips")
	if err != nil {
		return nil, err
	}
	minSpread, _, err := trader.GetFloat64Param(params, "min_spread")
	if err != nil {
		return nil, err
	}
	atrPeriod, _, err := trader.GetInt32Param(params, "atr_period")
	if err != nil {
		return nil, err
	}
	atrMult, _, err := trader.GetFloat64Param(params, "atr_multiplier")
	if err != nil {
		return nil, err
	}
	return New(Config{
		FastPeriod:    int(fast),
		SlowPeriod:    int(slow),
		Scale:         trader.PriceScale,
		StopPips:      trader.PipsFromFloat(stopPips),
		MinSpread:     minSpread,
		ATRPeriod:     int(atrPeriod),
		ATRMultiplier: atrMult,
	})
}
