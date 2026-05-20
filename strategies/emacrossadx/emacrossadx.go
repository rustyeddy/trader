// Package emacrossadx implements the EMA-cross strategy with an ADX
// trend-strength gate. Registers under "ema-cross-adx".
package emacrossadx

import (
	"context"
	"fmt"
	"math"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/strategies/emacross"
)

func init() {
	trader.RegisterStrategy(build, "ema-cross-adx")
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
	trader.StrategyBaseConfig

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

func New(cfg Config) *Strategy {
	if cfg.FastPeriod <= 0 || cfg.SlowPeriod <= 0 || cfg.ADXPeriod <= 0 {
		panic("emacrossadx: periods must be > 0")
	}
	if cfg.FastPeriod >= cfg.SlowPeriod {
		panic("emacrossadx: FastPeriod must be < SlowPeriod")
	}
	if cfg.Scale <= 0 {
		panic("emacrossadx: Scale must be > 0")
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
		atr = trader.NewATR(cfg.ATRPeriod, cfg.Scale)
	}

	return &Strategy{
		core: emacross.Core{
			Fast:          trader.NewEMA(cfg.FastPeriod, cfg.Scale),
			Slow:          trader.NewEMA(cfg.SlowPeriod, cfg.Scale),
			ATR:           atr,
			MinSpread:     cfg.MinSpread,
			Scale:         cfg.Scale,
			StopPips:      cfg.StopPips,
			ATRMultiplier: mult,
			Name:          fmt.Sprintf("EMA_CROSS_ADX(%d,%d,ADX%d@%.1f)", cfg.FastPeriod, cfg.SlowPeriod, cfg.ADXPeriod, cfg.ADXThreshold),
		},
		adx:             trader.NewADX(cfg.ADXPeriod, cfg.Scale),
		adxThreshold:    cfg.ADXThreshold,
		requireDI:       cfg.RequireDI,
		requireADXReady: cfg.RequireADXReady,
	}
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

func (x *Strategy) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	_ = ctx
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}
	c := ct.Candle
	x.core.Fast.Update(c)
	x.core.Slow.Update(c)
	x.adx.Update(c)
	if x.core.ATR != nil {
		x.core.ATR.Update(c)
	}

	fv := x.core.Fast.Float64()
	sv := x.core.Slow.Float64()
	dec := &trader.DefaultStrategyPlan

	if !x.core.Fast.Ready() || !x.core.Slow.Ready() {
		dec.Reason = "warming up EMAs"
		return dec
	}

	if x.requireADXReady && !x.adx.Ready() {
		dec.Reason = "warming up ADX"
		return dec
	}
	diff := fv - sv

	if x.core.MinSpread > 0 && math.Abs(diff) < x.core.MinSpread {
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

func build(params map[string]any) (trader.Strategy, error) {
	fast, ok, err := trader.GetInt32Param(params, "fast")
	if err != nil {
		return nil, err
	}
	if !ok || fast <= 0 {
		return nil, fmt.Errorf("ema-cross-adx: missing or invalid param %q", "fast")
	}
	slow, ok, err := trader.GetInt32Param(params, "slow")
	if err != nil {
		return nil, err
	}
	if !ok || slow <= 0 {
		return nil, fmt.Errorf("ema-cross-adx: missing or invalid param %q", "slow")
	}
	if fast >= slow {
		return nil, fmt.Errorf("ema-cross-adx: fast (%d) must be < slow (%d)", fast, slow)
	}
	adxPeriod, _, err := trader.GetInt32Param(params, "adx_period")
	if err != nil {
		return nil, err
	}
	if adxPeriod <= 0 {
		adxPeriod = 14
	}
	adxThreshold, _, err := trader.GetFloat64Param(params, "adx_threshold")
	if err != nil {
		return nil, err
	}
	if adxThreshold <= 0 {
		adxThreshold = 20.0
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
	requireDI, _, err := trader.GetBoolParam(params, "require_di")
	if err != nil {
		return nil, err
	}
	requireADXReady, ok, err := trader.GetBoolParam(params, "require_adx_ready")
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
	}), nil
}
