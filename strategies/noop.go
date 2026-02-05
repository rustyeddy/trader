package strategies

import (
	"context"

	"github.com/rustyeddy/trader/broker"
)

// NoopStrategy does nothing.
type NoopStrategy struct{}

func (NoopStrategy) OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error {
	_ = ctx
	_ = b
	_ = tick
	return nil
}
