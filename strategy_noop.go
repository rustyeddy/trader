package trader

import (
	"context"
)

// noopStrategy does nothing.
type noopStrategy struct{}

func (noopStrategy) Name() string            { return "NoOp" }
func (noopStrategy) Reset()                  {}
func (noopStrategy) Ready() bool             { return true }
func (noopStrategy) StopDescription() string { return "" }
func (noopStrategy) Update(ctx context.Context, c *CandleTime, run *Backtest) *StrategyPlan {
	_ = ctx
	_ = c
	return &DefaultStrategyPlan
}
