package broker

import (
	"context"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type Broker interface {
	GetAccount(ctx context.Context) (Account, error)
	GetTick(ctx context.Context, instrument string) (market.Tick, error)
	CreateMarketOrder(ctx context.Context, req MarketOrderRequest) (OrderFill, error)
}

// broker/models.go
type MarketOrderRequest struct {
	Instrument string
	Units      types.Units
	StopLoss   *types.Price
	TakeProfit *types.Price
}

type OrderFill struct {
	TradeID    string
	Instrument string
	Units      types.Units
	types.Price
}
