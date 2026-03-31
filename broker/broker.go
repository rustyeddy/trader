package broker

import (
	"context"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/types"
)

type Broker interface {
	Account(ctx context.Context) (account.Account, error)
	PlaceOrder(ctx context.Context, req OrderRequest) error
	ClosePosition(id string) error
}

// broker/models.go
type OrderRequest struct {
	Instrument string
	Units      types.Units
	StopLoss   *types.Price
	TakeProfit *types.Price
	Reason     string
}

type OrderFill struct {
	TradeID    string
	Instrument string
	Units      types.Units
	types.Price
}
