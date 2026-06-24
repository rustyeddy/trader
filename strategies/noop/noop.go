// Package noop implements a do-nothing strategy. Useful for plumbing tests
// and for measuring overhead. Registers under "noop" and "no-op".
package noop

import (
	"context"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(build, "noop", "no-op")
}

type Strategy struct{}

func (Strategy) Name() string            { return "NoOp" }
func (Strategy) Reset()                  {}
func (Strategy) Ready() bool             { return true }
func (Strategy) StopDescription() string { return "" }
func (Strategy) Update(ctx context.Context, c *market.CandleTime, run strategy.StrategyContext) *strategy.StrategyPlan {
	_ = ctx
	_ = c
	return strategy.DefaultPlan()
}

func build(params map[string]any) (strategy.Strategy, error) {
	return Strategy{}, nil
}
