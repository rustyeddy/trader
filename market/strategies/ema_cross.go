package strategies

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/market/indicators"
)

// EMACross generates signals when a fast EMA crosses a slow EMA.
// It uses a small state machine to avoid repeated signals while EMAs stay crossed.
type EMACross struct {
	fast *indicators.EMA
	slow *indicators.EMA

	// state: previous relationship between fast and slow
	// -1 => fast below slow, 0 => unknown/not-ready, +1 => fast above slow
	prevRel int
	name    string

	// optional filters
	minSpread float64 // require |fast-slow| >= minSpread (in price units) to signal
	scale     float64
}

type EMACrossConfig struct {
	StrategyConfig
	FastPeriod int
	SlowPeriod int
	Scale      int32

	// Optional: noise filter (in price units, e.g. 0.00005). 0 disables.
	MinSpread float64
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

	f := indicators.NewEMA(cfg.FastPeriod, cfg.Scale)
	s := indicators.NewEMA(cfg.SlowPeriod, cfg.Scale)

	return &EMACross{
		fast:      f,
		slow:      s,
		prevRel:   0,
		minSpread: cfg.MinSpread,
		name:      fmt.Sprintf("EMA_CROSS(%d,%d)", cfg.FastPeriod, cfg.SlowPeriod),
	}
}

func (x *EMACross) Name() string { return x.name }

func (x *EMACross) Reset() {
	x.fast.Reset()
	x.slow.Reset()
	x.prevRel = 0
}

func (x *EMACross) Ready() bool {
	return x.fast.Ready() && x.slow.Ready()
}

// Update consumes the next closed candle and returns a decision.
// Strategy emits a signal only on the *cross event* (state transition),
// not every candle while EMAs remain crossed.
func (x *EMACross) Update(c market.Candle) Decision {
	// Update indicators first
	x.fast.Update(c)
	x.slow.Update(c)

	close := float64(c.C) / float64(x.scale)

	// Not ready? no signal yet.
	if !x.Ready() {
		return EMACrossDecision{
			signal: Hold,
			reason: "warming up",
			Fast:   x.fast.Float64(),
			Slow:   x.slow.Float64(),
			Close:  close,
		}
	}

	fv := x.fast.Float64()
	sv := x.slow.Float64()
	diff := fv - sv

	// Optional noise filter
	if x.minSpread > 0 && abs(diff) < x.minSpread {
		return EMACrossDecision{
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

	// First time ready: establish baseline relationship, don't fire.
	if x.prevRel == 0 {
		x.prevRel = rel
		return EMACrossDecision{
			signal: Hold,
			reason: "baseline set",
			Fast:   fv,
			Slow:   sv,
			Close:  close,
		}
	}

	// Cross up: below -> above
	if x.prevRel == -1 && rel == +1 {
		x.prevRel = rel
		return EMACrossDecision{
			signal: Buy,
			reason: "fast EMA crossed above slow EMA",
			Fast:   fv,
			Slow:   sv,
			Close:  close,
		}
	}

	// Cross down: above -> below
	if x.prevRel == +1 && rel == -1 {
		x.prevRel = rel
		return EMACrossDecision{
			signal: Sell,
			reason: "fast EMA crossed below slow EMA",
			Fast:   fv,
			Slow:   sv,
			Close:  close,
		}
	}

	// No cross; maintain state
	x.prevRel = rel
	return EMACrossDecision{
		signal: Hold,
		reason: "no cross",
		Fast:   fv,
		Slow:   sv,
		Close:  close,
	}
}

type EMACrossDecision struct {
	signal Signal
	reason string

	Fast  float64
	Slow  float64
	Close float64
}

func (x EMACrossDecision) Signal() Signal {
	return x.signal
}

func (x EMACrossDecision) Reason() string {
	return x.reason
}
