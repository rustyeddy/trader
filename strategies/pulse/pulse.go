// Package pulse implements a mechanical candle-based strategy that opens and
// closes positions on a fixed bar schedule. It has no market analysis and is
// designed to validate the full strategy → adapter → live runner → broker
// pipeline against a demo account with minimal financial risk.
// Registers as "pulse" in the strategy registry.
package pulse

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
)

func init() {
	strategy.MustRegisterStrategy(build, "pulse")
}

// Config controls the pulse strategy's behaviour.
type Config struct {
	// TradeEvery opens a new position every N bars (1 = every bar).
	TradeEvery int `yaml:"trade_every"`

	// HoldBars closes all positions after any position has been open N bars.
	HoldBars int `yaml:"hold_bars"`

	// MaxPositions caps the number of concurrently open positions.
	MaxPositions int `yaml:"max_positions"`

	// Side controls direction: "long", "short", or "alternate" (default).
	Side string `yaml:"side"`

	// StopPips is the stop-loss distance in pips. Required (>0).
	StopPips float64 `yaml:"stop_pips"`
}

// DefaultConfig returns a safe, conservative default configuration.
func DefaultConfig() Config {
	return Config{
		TradeEvery:   5,
		HoldBars:     10,
		MaxPositions: 2,
		Side:         "alternate",
		StopPips:     20,
	}
}

// Strategy fires a trade every TradeEvery bars and closes all positions
// after any position has been held for HoldBars bars.
type Strategy struct {
	cfg         Config
	barCount    int
	lotOpenedAt map[string]int // lot ID → barCount when first seen
	sideTurn    int
}

// New creates a Strategy from the given Config.
func New(cfg Config) (*Strategy, error) {
	if cfg.TradeEvery <= 0 {
		cfg.TradeEvery = 1
	}
	if cfg.HoldBars <= 0 {
		return nil, fmt.Errorf("pulse: hold_bars must be > 0")
	}
	if cfg.MaxPositions <= 0 {
		return nil, fmt.Errorf("pulse: max_positions must be > 0")
	}
	if cfg.StopPips <= 0 {
		return nil, fmt.Errorf("pulse: stop_pips must be > 0")
	}
	side := strings.ToLower(strings.TrimSpace(cfg.Side))
	if side == "" {
		side = "alternate"
	}
	switch side {
	case "long", "short", "alternate":
	default:
		return nil, fmt.Errorf("pulse: side must be 'long', 'short', or 'alternate', got %q", cfg.Side)
	}
	cfg.Side = side
	return &Strategy{cfg: cfg}, nil
}

func (s *Strategy) Name() string { return "pulse" }

func (s *Strategy) Ready() bool { return true }

func (s *Strategy) Reset() {
	s.barCount = 0
	s.lotOpenedAt = nil
	s.sideTurn = 0
}

func (s *Strategy) StopDescription() string {
	return fmt.Sprintf("%.1f pips", s.cfg.StopPips)
}

// Update is called on every completed bar. It:
//  1. Tracks how many bars each open lot has been held.
//  2. Signals CloseAll when any lot has been held >= HoldBars bars.
//  3. Signals a new open every TradeEvery bars when under MaxPositions.
func (s *Strategy) Update(_ context.Context, ct *market.Candle, sctx strategy.StrategyContext) strategy.Signal {
	if ct == nil {
		return strategy.Hold("no candle")
	}
	s.barCount++

	if s.lotOpenedAt == nil {
		s.lotOpenedAt = map[string]int{}
	}

	// Sync lot tracking with currently open positions.
	openCount := 0
	shouldClose := false
	if sctx != nil {
		seen := map[string]bool{}
		_ = sctx.OpenLots().Range(func(lot *execution.Lot) error {
			openCount++
			seen[lot.ID] = true
			if _, tracked := s.lotOpenedAt[lot.ID]; !tracked {
				s.lotOpenedAt[lot.ID] = s.barCount
			}
			if s.barCount-s.lotOpenedAt[lot.ID] >= s.cfg.HoldBars {
				shouldClose = true
			}
			return nil
		})
		// Prune closed lots.
		for id := range s.lotOpenedAt {
			if !seen[id] {
				delete(s.lotOpenedAt, id)
			}
		}
	}

	activeAfterClose := openCount
	if shouldClose {
		activeAfterClose = 0
	}

	shouldOpen := s.barCount%s.cfg.TradeEvery == 0 && activeAfterClose < s.cfg.MaxPositions

	// Resolve instrument for pip-based stop computation.
	var inst *market.Instrument
	if sctx != nil {
		inst = market.GetInstrument(sctx.Instrument())
	}

	switch {
	case shouldClose && shouldOpen:
		side := s.nextSide()
		return strategy.Signal{
			Side:     side,
			CloseAll: true,
			Stop:     stopFromPips(ct, side, s.cfg.StopPips, inst),
			Reason:   "pulse-close-reopen",
		}
	case shouldClose:
		return strategy.Signal{
			Side:     types.Flat,
			CloseAll: true,
			Reason:   "pulse-close",
		}
	case shouldOpen:
		side := s.nextSide()
		return strategy.Signal{
			Side:   side,
			Stop:   stopFromPips(ct, side, s.cfg.StopPips, inst),
			Reason: "pulse-open",
		}
	default:
		return strategy.Hold("hold")
	}
}

// stopFromPips computes a stop price from a pip distance and candle close.
// Returns 0 when the instrument is unknown or stop_pips is not configured.
func stopFromPips(ct *market.Candle, side types.Side, stopPips float64, inst *market.Instrument) types.Price {
	if inst == nil || stopPips <= 0 || ct == nil {
		return 0
	}
	perPip := inst.PriceUnitsPerPip()
	if perPip <= 0 {
		return 0
	}
	dist := types.Price(math.Round(stopPips * float64(perPip)))
	if side == types.Long {
		return ct.Close - dist
	}
	return ct.Close + dist
}

func (s *Strategy) nextSide() types.Side {
	switch s.cfg.Side {
	case "long":
		return types.Long
	case "short":
		return types.Short
	default:
		side := types.Long
		if s.sideTurn%2 != 0 {
			side = types.Short
		}
		s.sideTurn++
		return side
	}
}

func build(params map[string]any) (strategy.Strategy, error) {
	cfg := DefaultConfig()
	if v, ok, err := strategy.GetInt32Param(params, "trade_every"); err != nil {
		return nil, err
	} else if ok {
		cfg.TradeEvery = int(v)
	}
	if v, ok, err := strategy.GetInt32Param(params, "hold_bars"); err != nil {
		return nil, err
	} else if ok {
		cfg.HoldBars = int(v)
	}
	if v, ok, err := strategy.GetInt32Param(params, "max_positions"); err != nil {
		return nil, err
	} else if ok {
		cfg.MaxPositions = int(v)
	}
	if v, ok, err := strategy.GetStringParam(params, "side"); err != nil {
		return nil, err
	} else if ok {
		cfg.Side = v
	}
	if v, ok, err := strategy.GetFloat64Param(params, "stop_pips"); err != nil {
		return nil, err
	} else if ok {
		cfg.StopPips = v
	}
	return New(cfg)
}
