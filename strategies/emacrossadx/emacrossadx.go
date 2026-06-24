// Package emacrossadx implements the EMA-cross strategy with an ADX
// trend-strength gate. Registers under "ema-cross-adx".
package emacrossadx

import (
	"context"
	"fmt"
	"math"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/strategies/emacross"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(build, "ema-cross-adx")
}

type Strategy struct {
	core emacross.Core
	adx  *trader.ADX

	adxThreshold    float64
	requireDI       bool
	requireADXReady bool
	pendingRel      int
}

type Config struct {
	FastPeriod      int
	SlowPeriod      int
	ADXPeriod       int
	Scale           trader.Scale6
	MinSpread       float64
	StopPips        trader.Pips
	ATRPeriod       int
	ATRMultiplier   float64
	ADXThreshold    float64
	RequireDI       bool
	RequireADXReady bool
}

func New(cfg Config) (*Strategy, error) {
	if cfg.FastPeriod <= 0 || cfg.SlowPeriod <= 0 || cfg.ADXPeriod <= 0 {
		return nil, fmt.Errorf("emacrossadx: periods must be > 0")
	}
	if cfg.FastPeriod >= cfg.SlowPeriod {
		return nil, fmt.Errorf("emacrossadx: FastPeriod must be < SlowPeriod")
	}
	if cfg.Scale <= 0 {
		return nil, fmt.Errorf("emacrossadx: Scale must be > 0")
	}
	if cfg.ADXThreshold <= 0 {
		cfg.ADXThreshold = 20.0
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
	adx, err := trader.NewADX(cfg.ADXPeriod, cfg.Scale)
	if err != nil {
		return nil, err
	}

	return &Strategy{
		core: emacross.Core{
			Fast:          fast,
			Slow:          slow,
			ATR:           atr,
			MinSpread:     trader.Price(math.Round(cfg.MinSpread * float64(cfg.Scale))),
			Scale:         cfg.Scale,
			StopPips:      cfg.StopPips,
			ATRMultiplier: mult,
			Name:          fmt.Sprintf("EMA_CROSS_ADX(%d,%d,ADX%d@%.1f)", cfg.FastPeriod, cfg.SlowPeriod, cfg.ADXPeriod, cfg.ADXThreshold),
		},
		adx:             adx,
		adxThreshold:    cfg.ADXThreshold,
		requireDI:       cfg.RequireDI,
		requireADXReady: cfg.RequireADXReady,
	}, nil
}

func (x *Strategy) Name() string            { return x.core.Name }
func (x *Strategy) StopDescription() string { return emacross.StopDesc(&x.core) }

func (x *Strategy) Reset() {
	x.core.Fast.Reset()
	x.core.Slow.Reset()
	x.adx.Reset()
	if x.core.ATR != nil {
		x.core.ATR.Reset()
	}
	x.core.PrevRel = 0
	x.pendingRel = 0
}

func (x *Strategy) Ready() bool {
	if !x.core.Fast.Ready() || !x.core.Slow.Ready() {
		return false
	}
	if x.requireADXReady && !x.adx.Ready() {
		return false
	}
	return true
}

func (x *Strategy) Update(ctx context.Context, ct *trader.CandleTime, run strategy.StrategyContext) *strategy.StrategyPlan {
	_ = ctx
	if ct == nil {
		return strategy.DefaultPlan()
	}
	c := ct.Candle
	x.core.Fast.Update(c)
	x.core.Slow.Update(c)
	x.adx.Update(c)
	if x.core.ATR != nil {
		x.core.ATR.Update(c)
	}

	dec := strategy.DefaultPlan()

	if !x.core.Fast.Ready() || !x.core.Slow.Ready() {
		dec.Reason = "warming up EMAs"
		return dec
	}

	if x.requireADXReady && !x.adx.Ready() {
		dec.Reason = "warming up ADX"
		return dec
	}
	diff := x.core.Fast.PriceSum() - x.core.Slow.PriceSum()

	if x.core.MinSpread > 0 && absPriceSum(diff) < trader.PriceSum(x.core.MinSpread) {
		dec.Reason = "min-spread filter"
		return dec
	}

	rel := 0
	if diff > 0 {
		rel = +1
	} else if diff < 0 {
		rel = -1
	}

	if x.core.PrevRel == 0 {
		if rel != 0 {
			x.core.PrevRel = rel
			dec.Reason = "baseline set"
		} else {
			dec.Reason = "baseline pending"
		}
		return dec
	}

	if x.core.PrevRel == -1 && rel == +1 {
		x.pendingRel = +1
	}
	if x.core.PrevRel == +1 && rel == -1 {
		x.pendingRel = -1
	}

	x.core.PrevRel = rel

	if x.pendingRel == 0 {
		dec.Reason = "no cross"
		return dec
	}

	if x.requireADXReady && !x.adx.Ready() {
		dec.Reason = "waiting for ADX readiness"
		return dec
	}

	if x.adx.Ready() && x.adx.Float64() < x.adxThreshold {
		dec.Reason = "waiting for ADX threshold"
		return dec
	}

	if x.requireDI && x.adx.Ready() {
		if x.pendingRel == +1 && !(x.adx.PlusDI() > x.adx.MinusDI()) {
			dec.Reason = "waiting for DI confirmation (buy)"
			return dec
		}
		if x.pendingRel == -1 && !(x.adx.MinusDI() > x.adx.PlusDI()) {
			dec.Reason = "waiting for DI confirmation (sell)"
			return dec
		}
	}

	if x.core.ATR != nil && !x.core.ATR.Ready() {
		dec.Reason = "warming up ATR"
		return dec
	}

	if x.pendingRel == +1 {
		x.pendingRel = 0
		plan := emacross.EmitOpen(&x.core, ct, run, trader.Long)
		plan.Reason = "EMA cross up + ADX confirmed"
		return plan
	}

	if x.pendingRel == -1 {
		x.pendingRel = 0
		plan := emacross.EmitOpen(&x.core, ct, run, trader.Short)
		plan.Reason = "EMA cross down + ADX confirmed"
		return plan
	}

	dec.Reason = "no cross"
	return dec
}

func absPriceSum(v trader.PriceSum) trader.PriceSum {
	if v < 0 {
		return -v
	}
	return v
}

func build(params map[string]any) (strategy.Strategy, error) {
	fast, ok, err := strategy.GetInt32Param(params, "fast")
	if err != nil {
		return nil, err
	}
	if !ok || fast <= 0 {
		return nil, fmt.Errorf("ema-cross-adx: missing or invalid param %q", "fast")
	}
	slow, ok, err := strategy.GetInt32Param(params, "slow")
	if err != nil {
		return nil, err
	}
	if !ok || slow <= 0 {
		return nil, fmt.Errorf("ema-cross-adx: missing or invalid param %q", "slow")
	}
	if fast >= slow {
		return nil, fmt.Errorf("ema-cross-adx: fast (%d) must be < slow (%d)", fast, slow)
	}
	adxPeriod, _, err := strategy.GetInt32Param(params, "adx_period")
	if err != nil {
		return nil, err
	}
	if adxPeriod <= 0 {
		adxPeriod = 14
	}
	adxThreshold, _, err := strategy.GetFloat64Param(params, "adx_threshold")
	if err != nil {
		return nil, err
	}
	if adxThreshold <= 0 {
		adxThreshold = 20.0
	}
	stopPips, _, err := strategy.GetFloat64Param(params, "stop_pips")
	if err != nil {
		return nil, err
	}
	minSpread, _, err := strategy.GetFloat64Param(params, "min_spread")
	if err != nil {
		return nil, err
	}
	atrPeriod, _, err := strategy.GetInt32Param(params, "atr_period")
	if err != nil {
		return nil, err
	}
	atrMult, _, err := strategy.GetFloat64Param(params, "atr_multiplier")
	if err != nil {
		return nil, err
	}
	requireDI, _, err := strategy.GetBoolParam(params, "require_di")
	if err != nil {
		return nil, err
	}
	requireADXReady, ok, err := strategy.GetBoolParam(params, "require_adx_ready")
	if err != nil {
		return nil, err
	}
	if !ok {
		requireADXReady = true
	}
	return New(Config{
		FastPeriod:      int(fast),
		SlowPeriod:      int(slow),
		ADXPeriod:       int(adxPeriod),
		Scale:           trader.PriceScale,
		StopPips:        trader.PipsFromFloat(stopPips),
		MinSpread:       minSpread,
		ATRPeriod:       int(atrPeriod),
		ATRMultiplier:   atrMult,
		ADXThreshold:    adxThreshold,
		RequireDI:       requireDI,
		RequireADXReady: requireADXReady,
	})
}
