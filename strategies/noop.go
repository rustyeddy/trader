package strategies

import (
	"context"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/pricing"
)

// NoopStrategy does nothing.
type NoopStrategy struct{}

func (NoopStrategy) OnTick(ctx context.Context, b broker.Broker, tick pricing.Tick) error {
	_ = ctx
	_ = b
	_ = tick
	return nil
}
