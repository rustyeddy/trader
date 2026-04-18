package trader

import (
	"context"
)

// NoopStrategy does nothing.
type NoopStrategy struct{}

func (NoopStrategy) Name() string   { return "NoOp" }
func (NoopStrategy) Reason() string { return "No-op" }
func (NoopStrategy) Update(ctx context.Context, c *Candle) *StrategyPlan {
	return &DefaultStrategyPlan
}
