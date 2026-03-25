package strategies

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/market/indicators"
	"github.com/rustyeddy/trader/types"
)

type EMACrossADX struct {
	core emaCrossCore
	adx  *indicators.ADX

	// these are the config
	adxThreshold    float64
	requireDI       bool
	requireADXReady bool

	minSpread float64 // optional EMA diff filter in price units
	name      string
}

type EMACrossADXConfig struct {
	FastPeriod      int
	SlowPeriod      int
	ADXPeriod       int
	Scale           types.Scale6
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
			fast: indicators.NewEMA(cfg.FastPeriod, cfg.Scale),
			slow: indicators.NewEMA(cfg.SlowPeriod, cfg.Scale),

			prevRel:   0,
			minSpread: cfg.MinSpread,
			name:      fmt.Sprintf("EMA_CROSS_ADX(%d,%d,ADX%d@%.1f)", cfg.FastPeriod, cfg.SlowPeriod, cfg.ADXPeriod, cfg.ADXThreshold),
		},
		adx:             indicators.NewADX(cfg.ADXPeriod, cfg.Scale),
		adxThreshold:    cfg.ADXThreshold,
		requireDI:       cfg.RequireDI,
		requireADXReady: cfg.RequireADXReady,
	}
}

func (x *EMACrossADX) Name() string { return x.name }

func (x *EMACrossADX) Reset() {
	x.core.fast.Reset()
	x.core.slow.Reset()
	x.adx.Reset()
	x.core.prevRel = 0
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

func (x *EMACrossADX) Update(c market.Candle) Decision {
	x.core.fast.Update(c)
	x.core.slow.Update(c)
	x.adx.Update(c)

	close := float64(c.Close) / float64(x.core.scale)

	// If EMAs aren't ready, we can't do cross logic.
	if !x.core.fast.Ready() || !x.core.slow.Ready() {
		return EMACrossADXDecision{
			signal: Hold,
			reason: "warming up EMAs",
			Fast:   x.core.fast.Float64(),
			Slow:   x.core.slow.Float64(),
			Close:  close,
		}
	}

	// Optionally require ADX to be ready before any signals.
	if x.requireADXReady && !x.adx.Ready() {
		return EMACrossADXDecision{
			signal: Hold,
			reason: "warming up ADX",
			Fast:   x.core.fast.Float64(),
			Slow:   x.core.slow.Float64(),
			Close:  close,
		}
	}

	fv := x.core.fast.Float64()
	sv := x.core.slow.Float64()
	diff := fv - sv

	// Optional noise filter on EMA spread
	if x.core.minSpread > 0 && abs(diff) < x.core.minSpread {
		return EMACrossADXDecision{
			signal: Hold,
			reason: "min-spread filter",
			Fast:   fv,
			Slow:   sv,
			Close:  close,
		}
	}

	rel := 0
	if diff > 0 {
		rel = +1
	} else if diff < 0 {
		rel = -1
	}

	// Baseline behavior: don't emit on first usable relationship.
	// If rel==0, keep waiting.
	if x.core.prevRel == 0 {
		if rel != 0 {
			x.core.prevRel = rel
			return EMACrossADXDecision{
				signal: Hold,
				reason: "baseline set",
				Fast:   fv,
				Slow:   sv,
				Close:  close,
			}
		}
		return EMACrossADXDecision{
			signal: Hold,
			reason: "baseline pending",
			Fast:   fv,
			Slow:   sv,
			Close:  close,
		}
	}

	adxVal := x.adx.Float64()
	if x.adx.Ready() && adxVal < x.adxThreshold {
		// ADX gate: trend too weak
		x.core.prevRel = rel
		return EMACrossADXDecision{
			signal: Hold,
			reason: "ADX below threshold",
			Fast:   fv,
			Slow:   sv,
			Close:  close,
		}
	}

	// Cross up: below -> above
	if x.core.prevRel == -1 && rel == +1 {
		if x.requireDI && x.adx.Ready() && !(x.adx.PlusDI() > x.adx.MinusDI()) {
			x.core.prevRel = rel
			return EMACrossADXDecision{signal: Hold, reason: "DI confirmation failed (buy)", Fast: fv, Slow: sv, Close: close}
		}
		x.core.prevRel = rel
		return EMACrossADXDecision{signal: Buy, reason: "EMA cross up + ADX gate", Fast: fv, Slow: sv, Close: close}
	}

	// Cross down: above -> below
	if x.core.prevRel == +1 && rel == -1 {
		if x.requireDI && x.adx.Ready() && !(x.adx.MinusDI() > x.adx.PlusDI()) {
			x.core.prevRel = rel
			return EMACrossADXDecision{signal: Hold, reason: "DI confirmation failed (sell)", Fast: fv, Slow: sv, Close: close}
		}
		x.core.prevRel = rel
		return EMACrossADXDecision{signal: Sell, reason: "EMA cross down + ADX gate", Fast: fv, Slow: sv, Close: close}
	}

	x.core.prevRel = rel
	return EMACrossADXDecision{signal: Hold, reason: "no cross", Fast: fv, Slow: sv, Close: close}
}

type EMACrossADXDecision struct {
	signal Signal
	reason string

	Fast  float64
	Slow  float64
	Close float64
}

func (x EMACrossADXDecision) Signal() Signal {
	return x.signal
}

func (x EMACrossADXDecision) Reason() string {
	return x.reason
}
