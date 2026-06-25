// Package lifecycle is a deterministic canned strategy used to regression-test
// the full config→candles→strategy→Trader→Broker→Account→Trades→Result
// pipeline. Registers under "lifecycle-test".
package lifecycle

import (
	"context"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(build, "lifecycle-test")
}

// Strategy opens long on bar 1 and closes on bar 3.
type Strategy struct {
	bar    int
	opened bool
	closed bool

	Units    market.Units
	StopPips float64
}

func (s *Strategy) Name() string            { return "lifecycle-test" }
func (s *Strategy) StopDescription() string { return "" }

func (s *Strategy) Reset() {
	s.bar = 0
	s.opened = false
	s.closed = false
}

func (s *Strategy) Ready() bool { return true }

func (s *Strategy) Update(_ context.Context, c *market.CandleTime, run strategy.StrategyContext) strategy.Signal {
	if c == nil || run == nil {
		return strategy.Hold("hold")
	}

	s.bar++

	if s.StopPips <= 0 {
		s.StopPips = 20
	}
	if s.Units <= 0 {
		s.Units = 1000
	}

	if s.bar == 1 && !s.opened {
		s.opened = true
		return strategy.Signal{Side: market.Long, Reason: "lifecycle-test-open-long"}
	}

	if s.bar == 3 && s.opened && !s.closed {
		s.closed = true
		return strategy.Signal{CloseAll: true, Reason: "lifecycle-test-close-long"}
	}

	return strategy.Hold("hold")
}

func build(params map[string]any) (strategy.Strategy, error) {
	return &Strategy{
		Units:    1000,
		StopPips: 20,
	}, nil
}
