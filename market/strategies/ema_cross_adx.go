package strategies

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/market/indicators"
)

type EMACrossADX struct {
	fast *indicators.EMA
	slow *indicators.EMA
	adx  *indicators.ADX

	// prevRel tracks prior fast/slow relationship: -1 below, +1 above, 0 unknown
	prevRel int

	adxThreshold    float64
	requireDI       bool
	requireADXReady bool

	minSpread float64 // optional EMA diff filter in price units
	name      string
}

type EMACrossADXConfig struct {
	FastPeriod int
	SlowPeriod int
	ADXPeriod  int
	Scale      int32

	ADXThreshold    float64 // e.g. 20.0 or 25.0
	RequireDI       bool    // if true, confirm direction with DI (+DI>-DI for buy, opposite for sell)
	RequireADXReady bool    // if true, don't signal until ADX ready (recommended)

	MinSpread float64 // optional; 0 disables
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
		fast:            indicators.NewEMA(cfg.FastPeriod, cfg.Scale),
		slow:            indicators.NewEMA(cfg.SlowPeriod, cfg.Scale),
		adx:             indicators.NewADX(cfg.ADXPeriod, cfg.Scale),
		prevRel:         0,
		adxThreshold:    cfg.ADXThreshold,
		requireDI:       cfg.RequireDI,
		requireADXReady: cfg.RequireADXReady,
		minSpread:       cfg.MinSpread,
		name:            fmt.Sprintf("EMA_CROSS_ADX(%d,%d,ADX%d@%.1f)", cfg.FastPeriod, cfg.SlowPeriod, cfg.ADXPeriod, cfg.ADXThreshold),
	}
}

func (x *EMACrossADX) Name() string { return x.name }

func (x *EMACrossADX) Reset() {
	x.fast.Reset()
	x.slow.Reset()
	x.adx.Reset()
	x.prevRel = 0
}

func (x *EMACrossADX) Ready() bool {
	// EMA readiness is required. ADX readiness depends on config.
	if !x.fast.Ready() || !x.slow.Ready() {
		return false
	}
	if x.requireADXReady && !x.adx.Ready() {
		return false
	}
	return true
}

func (x *EMACrossADX) Update(c market.Candle, scale int32) Decision {
	x.fast.Update(c)
	x.slow.Update(c)
	x.adx.Update(c)

	close := float64(c.C) / float64(scale)

	// If EMAs aren't ready, we can't do cross logic.
	if !x.fast.Ready() || !x.slow.Ready() {
		return Decision{
			Signal: Hold,
			Reason: "warming up EMAs",
			Fast:   x.fast.Float64(),
			Slow:   x.slow.Float64(),
			Close:  close,
		}
	}

	// Optionally require ADX to be ready before any signals.
	if x.requireADXReady && !x.adx.Ready() {
		return Decision{
			Signal: Hold,
			Reason: "warming up ADX",
			Fast:   x.fast.Float64(),
			Slow:   x.slow.Float64(),
			Close:  close,
		}
	}

	fv := x.fast.Float64()
	sv := x.slow.Float64()
	diff := fv - sv

	// Optional noise filter on EMA spread
	if x.minSpread > 0 && abs(diff) < x.minSpread {
		return Decision{
			Signal: Hold,
			Reason: "min-spread filter",
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
	if x.prevRel == 0 {
		if rel != 0 {
			x.prevRel = rel
			return Decision{
				Signal: Hold,
				Reason: "baseline set",
				Fast:   fv,
				Slow:   sv,
				Close:  close,
			}
		}
		return Decision{
			Signal: Hold,
			Reason: "baseline pending",
			Fast:   fv,
			Slow:   sv,
			Close:  close,
		}
	}

	adxVal := x.adx.Float64()
	if x.adx.Ready() && adxVal < x.adxThreshold {
		// ADX gate: trend too weak
		x.prevRel = rel
		return Decision{
			Signal: Hold,
			Reason: "ADX below threshold",
			Fast:   fv,
			Slow:   sv,
			Close:  close,
		}
	}

	// Cross up: below -> above
	if x.prevRel == -1 && rel == +1 {
		if x.requireDI && x.adx.Ready() && !(x.adx.PlusDI() > x.adx.MinusDI()) {
			x.prevRel = rel
			return Decision{Signal: Hold, Reason: "DI confirmation failed (buy)", Fast: fv, Slow: sv, Close: close}
		}
		x.prevRel = rel
		return Decision{Signal: Buy, Reason: "EMA cross up + ADX gate", Fast: fv, Slow: sv, Close: close}
	}

	// Cross down: above -> below
	if x.prevRel == +1 && rel == -1 {
		if x.requireDI && x.adx.Ready() && !(x.adx.MinusDI() > x.adx.PlusDI()) {
			x.prevRel = rel
			return Decision{Signal: Hold, Reason: "DI confirmation failed (sell)", Fast: fv, Slow: sv, Close: close}
		}
		x.prevRel = rel
		return Decision{Signal: Sell, Reason: "EMA cross down + ADX gate", Fast: fv, Slow: sv, Close: close}
	}

	x.prevRel = rel
	return Decision{Signal: Hold, Reason: "no cross", Fast: fv, Slow: sv, Close: close}
}
