package strategies

import (
	"context"

	"github.com/rustyeddy/trader/types"
)

// NoopStrategy does nothing.
type NoopStrategy struct{}

func (NoopStrategy) Name() string   { return "NoOp" }
func (NoopStrategy) Reason() string { return "No-op" }
func (NoopStrategy) Update(ctx context.Context, c *types.Candle) *Plan {
	return &DefaultPlan
}
