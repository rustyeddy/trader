// Package tmpl is a strategy template / starting point for new strategy
// implementations. Returns a fresh default plan; copy and edit to build a
// new strategy. Registers under "template".
package tmpl

import (
	"context"
	"fmt"
	"math"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
)

func init() {
	strategy.MustRegisterStrategy(build, "template")
}

type Config struct {
	Lookback  int
	Threshold float64 // price-change threshold in native units (e.g. 0.0015 for 15 pips on a 5-decimal pair)
	Scale     types.Scale6
}

type Strategy struct {
	cfg       Config
	name      string
	threshold types.Price // Threshold converted to Price units at construction

	ready     bool
	bars      int
	lastClose types.Price
}

func New(cfg Config) (*Strategy, error) {
	if cfg.Lookback <= 0 {
		return nil, fmt.Errorf("tmpl: Lookback must be > 0")
	}
	if cfg.Scale <= 0 {
		return nil, fmt.Errorf("tmpl: Scale must be > 0")
	}

	return &Strategy{
		cfg:       cfg,
		threshold: types.Price(math.Round(cfg.Threshold * float64(cfg.Scale))),
		name:      fmt.Sprintf("TEMPLATE_STRATEGY(lb=%d,th=%.4f)", cfg.Lookback, cfg.Threshold),
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

func (s *Strategy) Update(_ context.Context, ct *market.Candle, _ strategy.StrategyContext) strategy.Signal {
	if ct == nil {
		return strategy.Hold("no candle")
	}
	closePx := ct.Close

	s.bars++
	if s.bars < s.cfg.Lookback {
		s.lastClose = closePx
		return strategy.Hold("warming up")
	}

	s.ready = true

	if s.lastClose > 0 {
		change := closePx - s.lastClose
		if change > s.threshold {
			s.lastClose = closePx
			return strategy.Hold("above threshold")
		}
		if change < -s.threshold {
			s.lastClose = closePx
			return strategy.Hold("below threshold")
		}
	}

	s.lastClose = closePx
	return strategy.Hold("hold")
}

func build(params map[string]any) (strategy.Strategy, error) {
	return New(Config{
		Lookback:  5,
		Threshold: 0.0015,
		Scale:     types.PriceScale,
	})
}
