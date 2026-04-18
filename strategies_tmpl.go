package trader

import (
	"fmt"
)

type TemplateStrategyConfig struct {
	StrategyBaseConfig

	// Strategy-specific parameters
	Lookback  int
	Threshold float64
	Scale     Scale6
}

type TemplateStrategy struct {
	cfg TemplateStrategyConfig

	name string

	// internal state
	ready bool
	bars  int

	// example rolling state
	lastClose float64
}

func NewTemplateStrategy(cfg TemplateStrategyConfig) *TemplateStrategy {
	if cfg.Lookback <= 0 {
		panic("TemplateStrategy requires Lookback > 0")
	}
	if cfg.Scale <= 0 {
		panic("TemplateStrategy requires Scale > 0")
	}

	return &TemplateStrategy{
		cfg:  cfg,
		name: fmt.Sprintf("TEMPLATE_STRATEGY(lb=%d,th=%.4f)", cfg.Lookback, cfg.Threshold),
	}
}

func (s *TemplateStrategy) Name() string { return s.name }

func (s *TemplateStrategy) Reset() {
	s.ready = false
	s.bars = 0
	s.lastClose = 0
}

func (s *TemplateStrategy) Ready() bool {
	return s.ready
}

func (s *TemplateStrategy) Update(c Candle) *StrategyPlan {
	closePx := float64(c.Close) / float64(s.cfg.Scale)

	s.bars++
	if s.bars < s.cfg.Lookback {
		s.lastClose = closePx
		return &DefaultStrategyPlan
	}

	s.ready = true

	// Replace this with real logic.
	// Example placeholder:
	if s.lastClose > 0 {
		change := closePx - s.lastClose
		if change > s.cfg.Threshold {
			s.lastClose = closePx
			return &DefaultStrategyPlan
		}

		if change < -s.cfg.Threshold {
			s.lastClose = closePx
			return &DefaultStrategyPlan
		}
	}

	s.lastClose = closePx
	return &DefaultStrategyPlan

}
