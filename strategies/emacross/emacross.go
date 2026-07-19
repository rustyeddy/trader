// Package emacross implements the fast/slow EMA crossover strategy.
// Registers under "ema-cross".
package emacross

import (
	"context"
	"fmt"
	"math"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
)

func init() {
	strategy.MustRegisterStrategy(build, "ema-cross")
}

// Core holds the shared EMA-cross state used by both ema-cross and
// ema-cross-adx strategies. Exported so the ADX variant in a sibling
// package can reuse the open/stop logic.
type Core struct {
	Fast *indicator.EMA
	Slow *indicator.EMA
	ATR  *indicator.ATR // nil when ATR stop not configured

	Name          string
	PrevRel       int
	MinSpread     types.Price
	Scale         types.Scale6
	StopPips      types.Pips
	ATRMultiplier float64
}

// Cross generates signals when a fast EMA crosses a slow EMA.
type Cross struct {
	core Core
}

type Config struct {
	FastPeriod    int
	SlowPeriod    int
	Scale         types.Scale6
	MinSpread     float64
	StopPips      types.Pips
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

	var atr *indicator.ATR
	if cfg.ATRPeriod > 0 {
		var err error
		atr, err = indicator.NewATR(cfg.ATRPeriod, cfg.Scale)
		if err != nil {
			return nil, err
		}
	}

	fast, err := indicator.NewEMA(cfg.FastPeriod, cfg.Scale)
	if err != nil {
		return nil, err
	}
	slow, err := indicator.NewEMA(cfg.SlowPeriod, cfg.Scale)
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

func (x *Cross) Update(_ context.Context, ct *market.Candle, _ strategy.StrategyContext) strategy.Signal {
	if ct == nil {
		return strategy.Hold("no candle")
	}
	c := ct
	x.core.Fast.Update(*c)
	x.core.Slow.Update(*c)
	if x.core.ATR != nil {
		x.core.ATR.Update(*c)
	}

	if !x.Ready() {
		return strategy.Hold("warming up")
	}

	diff := x.core.Fast.PriceSum() - x.core.Slow.PriceSum()

	if x.core.MinSpread > 0 && absPriceSum(diff) < types.PriceSum(x.core.MinSpread) {
		return strategy.Hold("min-spread filter")
	}

	rel := 0
	if diff > 0 {
		rel = +1
	} else if diff < 0 {
		rel = -1
	}

	if x.core.PrevRel == 0 {
		x.core.PrevRel = rel
		return strategy.Hold("baseline set")
	}

	if x.core.PrevRel == -1 && rel == +1 {
		x.core.PrevRel = rel
		if x.core.ATR != nil && !x.core.ATR.Ready() {
			return strategy.Hold("warming up ATR")
		}
		return EmitOpen(types.Long, "ema-cross-up")
	}

	if x.core.PrevRel == +1 && rel == -1 {
		x.core.PrevRel = rel
		if x.core.ATR != nil && !x.core.ATR.Ready() {
			return strategy.Hold("warming up ATR")
		}
		return EmitOpen(types.Short, "ema-cross-down")
	}

	x.core.PrevRel = rel
	return strategy.Hold("no cross")
}

func absPriceSum(v types.PriceSum) types.PriceSum {
	if v < 0 {
		return -v
	}
	return v
}

// EmitOpen returns a directional Signal. The planner handles reversal-closes
// and open construction. Exported so emacrossadx can reuse it.
func EmitOpen(side types.Side, reason string) strategy.Signal {
	return strategy.Signal{Side: side, Reason: reason}
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

func priceFromFloat(v float64, scale types.Scale6) types.Price {
	return types.Price(math.Round(v * float64(scale)))
}

func build(params map[string]any) (strategy.Strategy, error) {
	fast, ok, err := types.GetInt32Param(params, "fast")
	if err != nil {
		return nil, err
	}
	if !ok || fast <= 0 {
		return nil, fmt.Errorf("ema-cross: missing or invalid param %q", "fast")
	}
	slow, ok, err := types.GetInt32Param(params, "slow")
	if err != nil {
		return nil, err
	}
	if !ok || slow <= 0 {
		return nil, fmt.Errorf("ema-cross: missing or invalid param %q", "slow")
	}
	if fast >= slow {
		return nil, fmt.Errorf("ema-cross: fast (%d) must be < slow (%d)", fast, slow)
	}
	stopPips, _, err := types.GetFloat64Param(params, "stop_pips")
	if err != nil {
		return nil, err
	}
	minSpread, _, err := types.GetFloat64Param(params, "min_spread")
	if err != nil {
		return nil, err
	}
	atrPeriod, _, err := types.GetInt32Param(params, "atr_period")
	if err != nil {
		return nil, err
	}
	atrMult, _, err := types.GetFloat64Param(params, "atr_multiplier")
	if err != nil {
		return nil, err
	}
	return New(Config{
		FastPeriod:    int(fast),
		SlowPeriod:    int(slow),
		Scale:         types.PriceScale,
		StopPips:      types.PipsFromFloat(stopPips),
		MinSpread:     minSpread,
		ATRPeriod:     int(atrPeriod),
		ATRMultiplier: atrMult,
	})
}
