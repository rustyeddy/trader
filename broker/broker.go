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
	Balance     float64
	Equity      float64
	MarginUsed  float64
	FreeMargin  float64
	MarginLevel float64
}

// broker/models.go
type MarketOrderRequest struct {
	Instrument string
	Units      float64
	StopLoss   *float64
	TakeProfit *float64
}

type OrderFill struct {
	TradeID    string
	Instrument string
	Units      float64
	Price      float64
}
