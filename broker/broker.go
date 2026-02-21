package broker

import (
	"context"

	"github.com/rustyeddy/trader/market"
)

type Broker interface {
	GetAccount(ctx context.Context) (Account, error)
	GetTick(ctx context.Context, instrument string) (market.Tick, error)
	CreateMarketOrder(ctx context.Context, req MarketOrderRequest) (OrderFill, error)
}

type Account struct {
	ID          string
	Currency    string
	Balance     market.Cash
	Equity      market.Cash
	MarginUsed  market.Cash
	FreeMargin  market.Cash
	MarginLevel market.Cash
}

// broker/models.go
type MarketOrderRequest struct {
	Instrument string
	Units      market.Units
	StopLoss   *market.Price
	TakeProfit *market.Price
}

type OrderFill struct {
	TradeID    string
	Instrument string
	Units      market.Units
	market.Price
}
