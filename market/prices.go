package market

import (
	"context"
	"github.com/rustyeddy/trader/broker"
)


type PriceSource interface {
	GetPrice(ctx context.Context, instrument string) (broker.Price, error)
}
