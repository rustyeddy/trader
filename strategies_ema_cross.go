package trader

import (
	"context"
	"fmt"
)

type emaCrossCore struct {
	fast *EMA
	slow *EMA

	name      string
	prevRel   int
	minSpread float64
	scale     Scale6
}

// EMACross generates signals when a fast EMA crosses a slow EMA.
// It uses a small state machine to avoid repeated signals while EMAs stay crossed.
type EMACross struct {
	// cfg  EMACrossConfig
	core emaCrossCore
}

type EMACrossConfig struct {
	StrategyBaseConfig

	FastPeriod int
	SlowPeriod int
	Scale      Scale6
	MinSpread  float64
}

func NewEMACross(cfg EMACrossConfig) *EMACross {

	if cfg.FastPeriod <= 0 || cfg.SlowPeriod <= 0 {
		panic("EMACross periods must be > 0")
	}
	if cfg.FastPeriod >= cfg.SlowPeriod {
		// not strictly required, but common and avoids confusing configs
		panic("EMACross requires FastPeriod < SlowPeriod")
	}
	if cfg.Scale <= 0 {
		panic("EMACross requires Scale > 0")
	}

	f := NewEMA(cfg.FastPeriod, cfg.Scale) // Scale6 (PriceScale should be a default
	s := NewEMA(cfg.SlowPeriod, cfg.Scale)

	return &EMACross{
		core: emaCrossCore{
			fast:      f,
			slow:      s,
			prevRel:   0,
			minSpread: cfg.MinSpread,
			scale:     cfg.Scale, // <-- REQUIRE
			name:      fmt.Sprintf("EMA_CROSS(%d,%d)", cfg.FastPeriod, cfg.SlowPeriod),
		},
	}
}

func (x *EMACross) Name() string { return x.core.name }

func (x *EMACross) Reset() {
	x.core.fast.Reset()
	x.core.slow.Reset()
	x.core.prevRel = 0
}

func (x *EMACross) Ready() bool {
	return x.core.fast.Ready() && x.core.slow.Ready()
}

// Update consumes the next closed candle and returns a decision.
// Strategy emits a signal only on the *cross event* (state transition),
// not every candle while EMAs remain crossed.
func (x *EMACross) Update(ctx context.Context, ct *CandleTime, positions *Positions) *StrategyPlan {
	_ = ctx
	_ = positions
	if ct == nil {
		return &DefaultStrategyPlan
	}
	c := ct.Candle
	// Update indicators first
	x.core.fast.Update(c)
	x.core.slow.Update(c)

	// close := float64(c.Close) / float64(x.core.scale)

	// Not ready? no signal yet.
	if !x.Ready() {
		return &DefaultStrategyPlan
	}

	fv := x.core.fast.Float64()
	sv := x.core.slow.Float64()
	diff := fv - sv

	// Optional noise filter
	if x.core.minSpread > 0 && abs(diff) < x.core.minSpread {
		return &DefaultStrategyPlan
	}

	rel := 0
	if diff > 0 {
		rel = +1
	} else if diff < 0 {
		rel = -1
	}

	// First time ready: establish baseline relationship, don't fire.
	if x.core.prevRel == 0 {
		x.core.prevRel = rel
		return &DefaultStrategyPlan
	}

	// Cross up: below -> above
	if x.core.prevRel == -1 && rel == +1 {
		x.core.prevRel = rel
		return &DefaultStrategyPlan
	}

	// Cross down: above -> below
	if x.core.prevRel == +1 && rel == -1 {
		x.core.prevRel = rel
		return &DefaultStrategyPlan
	}

	// No cross; maintain state
	x.core.prevRel = rel
	return &DefaultStrategyPlan
}
