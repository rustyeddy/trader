package backtest

import (
	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/types"
)

// BacktestRun holds mutable state accumulated during a single backtest
// execution: the current lot book, the list of closed trades, and execution-cost
// counters updated by the run loop.
type BacktestRun struct {
	Lots   *account.LotBook
	Trades []*account.Trade

	// Execution cost tracking — populated by the run loop.
	SpreadFiltered int         // opens suppressed by the max-spread filter
	SpreadOpened   int         // opens that went through (for avg spread calc)
	SpreadSum      types.Price // sum of candle.AvgSpread at each accepted open
}

// GetTrades returns the run's closed trade list, or nil if run is nil.
func (run *BacktestRun) GetTrades() []*account.Trade {
	if run == nil {
		return nil
	}
	return run.Trades
}
