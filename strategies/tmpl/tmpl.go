// Package tmpl is a strategy template / starting point for new strategy
// implementations. Returns a fresh default plan; copy and edit to build a
// new strategy. Registers under "template".
package tmpl

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(build, "template")
}

type Config struct {
	Lookback  int
	Threshold float64
	Scale     market.Scale6
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

func (s *Strategy) Update(ctx context.Context, ct *market.CandleTime, run strategy.StrategyContext) *strategy.StrategyPlan {
	if ct == nil {
		return strategy.DefaultPlan()
	}
	c := ct.Candle
	closePx := float64(c.Close) / float64(s.cfg.Scale)

	s.bars++
	if s.bars < s.cfg.Lookback {
		s.lastClose = closePx
		return strategy.DefaultPlan()
	}

	s.ready = true

	if s.lastClose > 0 {
		change := closePx - s.lastClose
		if change > s.cfg.Threshold {
			s.lastClose = closePx
			return strategy.DefaultPlan()
		}
		if change < -s.cfg.Threshold {
			s.lastClose = closePx
			return strategy.DefaultPlan()
		}
	}

	s.lastClose = closePx
	return strategy.DefaultPlan()
}

func build(params map[string]any) (strategy.Strategy, error) {
	return New(Config{
		Lookback:  5,
		Threshold: 0.0015,
		Scale:     market.PriceScale,
	})
}
