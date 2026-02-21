package sim

import (
	"github.com/rustyeddy/trader/market"
	_ "github.com/rustyeddy/trader/market"
)

type Trade struct {
	ID         string
	Instrument string
	Units      market.Units
	EntryPrice market.Price
	OpenTime   market.Timestamp

	StopLoss   *market.Price
	TakeProfit *market.Price

	// Realized
	ClosePrice market.Price
	CloseTime  market.Price
	RealizedPL market.Cash // account currency
	Open       bool
}

func (t *Trade) triggerStopLoss(price int32) bool {
	if t.StopLoss == nil {
		return false
	}
	if t.Units > 0 {
		return price <= *t.StopLoss
	}
	return price >= *t.StopLoss
}

func (t *Trade) triggerTakeProfit(price int32) bool {
	if t.TakeProfit == nil {
		return false
	}
	if t.Units > 0 {
		return price >= *t.TakeProfit
	}
	return price <= *t.TakeProfit
}

func (t *Trade) UnrealizedPL(currentPrice float64, quoteToAccount float64) float64 {
	plQuote := t.Units * (currentPrice - t.EntryPrice)
	return plQuote * quoteToAccount
}
