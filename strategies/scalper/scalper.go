// Package scalper is a scaffold for a fast EMA-cross live strategy designed
// to generate many trades per day on M1 candles for broker integration testing.
// Registers as "scalper" in the strategy registry.
package scalper

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.RegisterStrategy(build, "scalper")
}

// Config holds scalper parameters.
type Config struct {
	trader.StrategyBaseConfig

	FastPeriod int
	SlowPeriod int
}

// Strategy implements trader.Strategy. Update is a no-op scaffold; signal
// logic will be added once the candle delivery pipeline is confirmed working.
type Strategy struct {
	cfg  Config
	name string
}

// New creates a Strategy. Returns an error for invalid config.
func New(cfg Config) (*Strategy, error) {
	if cfg.FastPeriod <= 0 {
		return nil, fmt.Errorf("scalper: fast_period must be > 0")
	}
	if cfg.SlowPeriod <= 0 {
		return nil, fmt.Errorf("scalper: slow_period must be > 0")
	}
	if cfg.FastPeriod >= cfg.SlowPeriod {
		return nil, fmt.Errorf("scalper: fast_period (%d) must be < slow_period (%d)",
			cfg.FastPeriod, cfg.SlowPeriod)
	}
	return &Strategy{
		cfg:  cfg,
		name: fmt.Sprintf("SCALPER(ema%d/%d)", cfg.FastPeriod, cfg.SlowPeriod),
	}, nil
}

func (s *Strategy) Name() string            { return s.name }
func (s *Strategy) StopDescription() string { return "" }
func (s *Strategy) Ready() bool             { return true }
func (s *Strategy) Reset()                  {}

// Update receives each completed M1 candle from the CandleStrategyAdapter.
// Currently a no-op scaffold — returns DefaultStrategyPlan on every bar.
func (s *Strategy) Update(_ context.Context, ct *trader.CandleTime, _ *trader.Backtest) *trader.StrategyPlan {
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}
	return &trader.DefaultStrategyPlan
}

func build(params map[string]any) (trader.Strategy, error) {
	fast, _, err := trader.GetInt32Param(params, "fast_period")
	if err != nil {
		return nil, err
	}
	if fast <= 0 {
		fast = 3
	}
	slow, _, err := trader.GetInt32Param(params, "slow_period")
	if err != nil {
		return nil, err
	}
	if slow <= 0 {
		slow = 8
	}
	return New(Config{FastPeriod: int(fast), SlowPeriod: int(slow)})
}
