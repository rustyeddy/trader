package sim

import "github.com/rustyeddy/trader/types"

type Trade struct {
	ID         string
	Instrument string
	Units      types.Units
	EntryPrice types.Price
	OpenTime   types.Timestamp

	StopLoss   *types.Price
	TakeProfit *types.Price

	// Realized
	ClosePrice types.Price
	CloseTime  types.Timestamp
	RealizedPL types.Money // account currency
	Open       bool
}

func (t *Trade) triggerStopLoss(price types.Price) bool {
	if t.StopLoss == nil {
		return false
	}
	if t.Units > 0 {
		return price <= *t.StopLoss
	}
	return price >= *t.StopLoss
}

func (t *Trade) triggerTakeProfit(price types.Price) bool {
	if t.TakeProfit == nil {
		return false
	}
	if t.Units > 0 {
		return price >= *t.TakeProfit
	}
	return price <= *t.TakeProfit
}

func (t *Trade) UnrealizedPL(currentPrice types.Price, quoteToAccount types.Price) types.Money {
	plQuote := types.Money(t.Units) * types.Money(currentPrice-t.EntryPrice)
	return types.Money(plQuote * types.Money(quoteToAccount))
}
