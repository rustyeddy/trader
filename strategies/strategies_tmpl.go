package strategies

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type TemplateStrategyConfig struct {
	StrategyConfig

	// Strategy-specific parameters
	Lookback  int
	Threshold float64
	Scale     types.Scale6
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

func (s *TemplateStrategy) Update(c market.Candle) Decision {
	closePx := float64(c.Close) / float64(s.cfg.Scale)

	s.bars++
	if s.bars < s.cfg.Lookback {
		s.lastClose = closePx
		return TemplateStrategyDecision{
			signal: Hold,
			reason: "warming up",
			Close:  closePx,
		}
	}

	s.ready = true

	// Replace this with real logic.
	// Example placeholder:
	if s.lastClose > 0 {
		change := closePx - s.lastClose
		if change > s.cfg.Threshold {
			s.lastClose = closePx
			return TemplateStrategyDecision{
				signal: Buy,
				reason: "threshold crossed up",
				Close:  closePx,
			}
		}
		if change < -s.cfg.Threshold {
			s.lastClose = closePx
			return TemplateStrategyDecision{
				signal: Sell,
				reason: "threshold crossed down",
				Close:  closePx,
			}
		}
	}

	s.lastClose = closePx
	return TemplateStrategyDecision{
		signal: Hold,
		reason: "no signal",
		Close:  closePx,
	}
}

type TemplateStrategyDecision struct {
	signal Signal
	reason string

	Close float64
}

func (d TemplateStrategyDecision) Signal() Signal { return d.signal }
func (d TemplateStrategyDecision) Reason() string { return d.reason }
