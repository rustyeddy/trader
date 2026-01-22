package market

import (
	"context"
	"github.com/rustyeddy/trader/pkg/broker"
)


type PriceSource interface {
	GetPrice(ctx context.Context, instrument string) (broker.Price, error)
}
