package broker

import (
	"context"
	"time"
)

type Account struct {
	ID       string
	Currency string
	Balance  float64
	Equity   float64
	MarginUsed   float64
	FreeMargin   float64
	MarginLevel  float64
}

type Price struct {
	Instrument string
	Bid        float64
	Ask        float64
	time.Time
}

func (p Price) Mid() float64 {
	return (p.Bid + p.Ask) / 2
}

// pkg/broker/models.go
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

type Broker interface {
	GetAccount(ctx context.Context) (Account, error)
	GetPrice(ctx context.Context, instrument string) (Price, error)
	CreateMarketOrder(ctx context.Context, req MarketOrderRequest) (OrderFill, error)
}
