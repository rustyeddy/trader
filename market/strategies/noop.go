package strategies

import (
	"context"

	"github.com/rustyeddy/trader/market"
)

// NoopStrategy does nothing.
type NoopStrategy struct{}

func (NoopStrategy) Name() string   { return "NoOp" }
func (NoopStrategy) Reason() string { return "No-op" }
func (NoopStrategy) Update(ctx context.Context, c *market.Candle) Decision {
	return nil
}
