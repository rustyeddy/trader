package backtest

import (
	"time"
)

// Result is a lightweight summary of a backtest run.
type Result struct {
	Balance float64
	Equity  float64

	Trades int
	Wins   int
	Losses int

	Start time.Time
	End   time.Time
}
