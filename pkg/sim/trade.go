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
