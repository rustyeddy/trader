// Package tmpl is a strategy template / starting point for new strategy
// implementations. Returns a fresh default plan; copy and edit to build a
// new strategy. Registers under "template".
package tmpl

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.MustRegisterStrategy(build, "template")
}

type Config struct {
	Lookback  int
	Threshold float64
	Scale     trader.Scale6
}

type Strategy struct {
	cfg  Config
	name string

	ready bool
	bars  int

	lastClose float64
}

func New(cfg Config) (*Strategy, error) {
	if cfg.Lookback <= 0 {
		return nil, fmt.Errorf("tmpl: Lookback must be > 0")
	}
	if cfg.Scale <= 0 {
		return nil, fmt.Errorf("tmpl: Scale must be > 0")
	}

	return &Strategy{
		cfg:  cfg,
		name: fmt.Sprintf("TEMPLATE_STRATEGY(lb=%d,th=%.4f)", cfg.Lookback, cfg.Threshold),
	}, nil
}

func (s *Strategy) Name() string            { return s.name }
func (s *Strategy) StopDescription() string { return "" }

func (s *Strategy) Reset() {
	s.ready = false
	s.bars = 0
	s.lastClose = 0
}

func (s *Strategy) Ready() bool { return s.ready }

func (s *Strategy) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	if ct == nil {
		return trader.DefaultPlan()
	}
	c := ct.Candle
	closePx := float64(c.Close) / float64(s.cfg.Scale)

	s.bars++
	if s.bars < s.cfg.Lookback {
		s.lastClose = closePx
		return trader.DefaultPlan()
	}

	s.ready = true

	if s.lastClose > 0 {
		change := closePx - s.lastClose
		if change > s.cfg.Threshold {
			s.lastClose = closePx
			return trader.DefaultPlan()
		}
		if change < -s.cfg.Threshold {
			s.lastClose = closePx
			return trader.DefaultPlan()
		}
	}

	s.lastClose = closePx
	return trader.DefaultPlan()
}

func build(params map[string]any) (trader.Strategy, error) {
	return New(Config{
		Lookback:  5,
		Threshold: 0.0015,
		Scale:     trader.PriceScale,
	})
}
