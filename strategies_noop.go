package trader

import (
	"context"
)

// noopStrategy does nothing.
type noopStrategy struct{}

func (noopStrategy) Name() string   { return "NoOp" }
func (noopStrategy) Reason() string { return "No-op" }
func (noopStrategy) Update(ctx context.Context, c *Candle) *StrategyPlan {
	return &DefaultStrategyPlan
}
