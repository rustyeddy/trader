package sim

import "time"

type Trade struct {
	ID         string
	Instrument string
	Units      float64
	EntryPrice float64
	OpenTime   time.Time

	StopLoss   *float64
	TakeProfit *float64

	// Realized
	ClosePrice float64
	CloseTime  time.Time
	RealizedPL float64 // account currency
	Open       bool
}

func (t *Trade) triggerStopLoss(price float64) bool {
	if t.StopLoss == nil {
		return false
	}
	if t.Units > 0 {
		return price <= *t.StopLoss
	}
	return price >= *t.StopLoss
}

func (t *Trade) triggerTakeProfit(price float64) bool {
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
