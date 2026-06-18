// Package pulse provides a mechanical live-trading strategy that opens and
// closes positions on a fixed schedule. It has no market analysis and is
// designed to validate the full strategy → live runner → broker pipeline
// against a demo account with minimal financial risk.
package pulse

import (
	"context"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.MustRegisterLiveStrategy(func(params map[string]any) (trader.LiveStrategy, error) {
		cfg := DefaultConfig()
		if v, ok, err := trader.GetInt32Param(params, "trade_every"); err != nil {
			return nil, err
		} else if ok {
			cfg.TradeEvery = int(v)
		}
		if v, ok, err := trader.GetInt32Param(params, "hold_bars"); err != nil {
			return nil, err
		} else if ok {
			cfg.HoldBars = int(v)
		}
		if v, ok, err := trader.GetInt32Param(params, "max_positions"); err != nil {
			return nil, err
		} else if ok {
			cfg.MaxPositions = int(v)
		}
		if v, ok, err := trader.GetStringParam(params, "side"); err != nil {
			return nil, err
		} else if ok {
			cfg.Side = v
		}
		if v, ok, err := trader.GetFloat64Param(params, "stop_pips"); err != nil {
			return nil, err
		} else if ok {
			cfg.StopPips = v
		}
		if v, ok, err := trader.GetFloat64Param(params, "take_pips"); err != nil {
			return nil, err
		} else if ok {
			cfg.TakePips = v
		}
		if v, ok, err := trader.GetFloat64Param(params, "risk_pct"); err != nil {
			return nil, err
		} else if ok {
			cfg.RiskPct = v
		}
		return New(cfg)
	}, "pulse")
}

// Config controls the pulse strategy's behaviour. All fields map directly to
// YAML params so the strategy is fully configurable without recompilation.
type Config struct {
	// TradeEvery opens a new position every N ticks (1 = every tick).
	TradeEvery int `yaml:"trade_every"`

	// HoldBars closes a position after it has been open for N ticks.
	HoldBars int `yaml:"hold_bars"`

	// MaxPositions caps the number of concurrently open positions.
	MaxPositions int `yaml:"max_positions"`

	// Side controls direction: "long", "short", or "alternate" (default).
	Side string `yaml:"side"`

	// StopPips is the stop-loss distance in pips. Required (>0).
	StopPips float64 `yaml:"stop_pips"`

	// TakePips is the take-profit distance in pips. 0 = no take-profit.
	TakePips float64 `yaml:"take_pips"`

	// RiskPct is the percentage of account NAV to risk per trade.
	RiskPct float64 `yaml:"risk_pct"`
}

// DefaultConfig returns a safe, conservative default configuration suitable
// for a first demo-account test run.
func DefaultConfig() Config {
	return Config{
		TradeEvery:   5,
		HoldBars:     10,
		MaxPositions: 2,
		Side:         "alternate",
		StopPips:     20,
		TakePips:     0,
		RiskPct:      0.1,
	}
}

// Strategy is a trader.LiveStrategy that opens a position every TradeEvery
// ticks and closes each position after HoldBars ticks, subject to MaxPositions.
type Strategy struct {
	cfg      Config
	tick     int // total ticks since start
	sideTurn int // used only when Side == "alternate"
}

// New creates a Strategy from the given Config. Returns an error if the
// config is invalid (e.g. StopPips <= 0, MaxPositions <= 0).
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

	if cfg.RiskPct <= 0 {
		cfg.RiskPct = 0.1
	}
	return &Strategy{cfg: cfg}, nil
}

// Name implements trader.LiveStrategy.
func (s *Strategy) Name() string { return "pulse" }

// Tick implements trader.LiveStrategy. It:
//  1. Closes any positions that have been open >= HoldBars ticks.
//  2. Opens a new position when tick % TradeEvery == 0 and active positions
//     (after pending closes) are below MaxPositions.
func (s *Strategy) Tick(_ context.Context, _ trader.LivePrice, openTrades []trader.LiveTrade) *trader.LivePlan {
	s.tick++

	// Phase 1: collect positions to close.
	closing := make(map[string]struct{})
	for _, t := range openTrades {
		if t.TicksOpen >= s.cfg.HoldBars {
			closing[t.ID] = struct{}{}
		}
	}
	closeIDs := make([]string, 0, len(closing))
	for id := range closing {
		closeIDs = append(closeIDs, id)
	}

	// Phase 2: decide whether to open a new position.
	activeAfterClose := len(openTrades) - len(closing)
	var open *trader.LiveOpenRequest

	if s.tick%s.cfg.TradeEvery == 0 && activeAfterClose < s.cfg.MaxPositions {
		side := s.nextSide()
		open = &trader.LiveOpenRequest{
			Side:     side,
			StopPips: s.cfg.StopPips,
			TakePips: s.cfg.TakePips,
			RiskPct:  s.cfg.RiskPct,
		}
	}

	reason := "hold"
	if len(closeIDs) > 0 || open != nil {
		parts := make([]string, 0, 2)
		if len(closeIDs) > 0 {
			parts = append(parts, fmt.Sprintf("close %d", len(closeIDs)))
		}
		if open != nil {
			parts = append(parts, fmt.Sprintf("open %s", open.Side))
		}
		reason = strings.Join(parts, " + ")
	}

	return &trader.LivePlan{
		Open:     open,
		CloseIDs: closeIDs,
		Reason:   reason,
	}
}

func (s *Strategy) nextSide() string {
	switch s.cfg.Side {
	case "long":
		return "long"
	case "short":
		return "short"
	default: // "alternate"
		side := "long"
		if s.sideTurn%2 != 0 {
			side = "short"
		}
		s.sideTurn++
		return side
	}
}
