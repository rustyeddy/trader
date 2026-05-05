package trader

import (
	"context"
	"fmt"
)

type EMACrossADX struct {
	core emaCrossCore
	adx  *ADX

	// these are the config
	adxThreshold    float64
	requireDI       bool
	requireADXReady bool
	pendingRel      int
}

type EMACrossADXConfig struct {
	StrategyBaseConfig

	FastPeriod      int
	SlowPeriod      int
	ADXPeriod       int
	Scale           Scale6
	MinSpread       float64
	ADXThreshold    float64
	RequireDI       bool
	RequireADXReady bool
}

func NewEMACrossADX(cfg EMACrossADXConfig) *EMACrossADX {
	if cfg.FastPeriod <= 0 || cfg.SlowPeriod <= 0 || cfg.ADXPeriod <= 0 {
		panic("periods must be > 0")
	}
	if cfg.FastPeriod >= cfg.SlowPeriod {
		panic("EMACrossADX requires FastPeriod < SlowPeriod")
	}
	if cfg.Scale <= 0 {
		panic("Scale must be > 0")
	}
	if cfg.ADXThreshold <= 0 {
		cfg.ADXThreshold = 20.0
	}

	return &EMACrossADX{
		core: emaCrossCore{
			fast: NewEMA(cfg.FastPeriod, cfg.Scale),
			slow: NewEMA(cfg.SlowPeriod, cfg.Scale),

			prevRel:   0,
			minSpread: cfg.MinSpread,
			scale:     cfg.Scale,
			name:      fmt.Sprintf("EMA_CROSS_ADX(%d,%d,ADX%d@%.1f)", cfg.FastPeriod, cfg.SlowPeriod, cfg.ADXPeriod, cfg.ADXThreshold),
		},
		adx:             NewADX(cfg.ADXPeriod, cfg.Scale),
		adxThreshold:    cfg.ADXThreshold,
		requireDI:       cfg.RequireDI,
		requireADXReady: cfg.RequireADXReady,
	}
}

func (x *EMACrossADX) Name() string {
	return x.core.name
}

func (x *EMACrossADX) Reset() {
	x.core.fast.Reset()
	x.core.slow.Reset()
	x.adx.Reset()
	x.core.prevRel = 0
	x.pendingRel = 0
}

func (x *EMACrossADX) Ready() bool {
	// EMA readiness is required. ADX readiness depends on config.
	if !x.core.fast.Ready() || !x.core.slow.Ready() {
		return false
	}
	if x.requireADXReady && !x.adx.Ready() {
		return false
	}
	return true
}

func (x *EMACrossADX) Update(ctx context.Context, ct *CandleTime, run *BacktestRun) *StrategyPlan {
	_ = ctx
	if ct == nil {
		return &DefaultStrategyPlan
	}
	c := ct.Candle
	x.core.fast.Update(c)
	x.core.slow.Update(c)
	x.adx.Update(c)

	fv := x.core.fast.Float64()
	sv := x.core.slow.Float64()
	// close := float64(c.Close) / float64(x.core.scale)
	dec := &DefaultStrategyPlan

	// If EMAs aren't ready, we can't do cross logic.
	if !x.core.fast.Ready() || !x.core.slow.Ready() {
		dec.Reason = "warming up EMAs"
		return dec
	}

	// Optionally require ADX to be ready before any signals.
	if x.requireADXReady && !x.adx.Ready() {
		dec.Reason = "warming up ADX"
		return dec
	}
	diff := fv - sv

	// Optional noise filter on EMA spread
	if x.core.minSpread > 0 && abs(diff) < x.core.minSpread {
		dec.Reason = "min-spread filter"
		return dec
	}

	rel := 0
	if diff > 0 {
		rel = +1
	} else if diff < 0 {
		rel = -1
	}

	// Baseline behavior: don't emit on first usable relationship.
	if x.core.prevRel == 0 {
		if rel != 0 {
			x.core.prevRel = rel
			dec.Reason = "baseline set"
		} else {
			dec.Reason = "baseline pending"
		}
		return dec
	}

	// Detect a fresh cross and mark it pending.
	if x.core.prevRel == -1 && rel == +1 {
		x.pendingRel = +1
	}
	if x.core.prevRel == +1 && rel == -1 {
		x.pendingRel = -1
	}

	// Always track the current EMA relationship.
	x.core.prevRel = rel

	// No pending trend change to confirm.
	if x.pendingRel == 0 {
		dec.Reason = "no cross"
		return dec
	}

	// Require ADX readiness if configured.
	if x.requireADXReady && !x.adx.Ready() {
		dec.Reason = "waiting for ADX readiness"
		return dec
	}

	// Require ADX strength once ready.
	if x.adx.Ready() && x.adx.Float64() < x.adxThreshold {
		dec.Reason = "waiting for ADX threshold"
		return dec
	}

	// Require DI confirmation if configured.
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

	// Confirmation passed: emit the pending signal once.
	if x.pendingRel == +1 {
		dec.Reason = "EMA cross up + ADX confirmed"
		return dec
	}

	if x.pendingRel == -1 {
		dec.Reason = "EMA cross down + ADX confirmed"
		return dec
	}

	dec.Reason = "no cross"
	return dec
}
