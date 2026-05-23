package trader

// BacktestRun holds mutable state accumulated during a single backtest
// execution: the live lot book, the list of closed trades, and execution-cost
// counters updated by the run loop.
type BacktestRun struct {
	Lots   *LotBook
	Trades []*Trade

	// Execution cost tracking — populated by the run loop.
	SpreadFiltered int   // opens suppressed by the max-spread filter
	SpreadOpened   int   // opens that went through (for avg spread calc)
	SpreadSum      Price // sum of candle.AvgSpread at each accepted open
}

// GetTrades returns the run's closed trade list, or nil if run is nil.
func (run *BacktestRun) GetTrades() []*Trade {
	if run == nil {
		return nil
	}
	return run.Trades
}

// BuildBacktestResult copies the account's closed trades into the run.
// Full result computation (win/loss counts, P/L) is done by Backtest.BuildBacktestResult.
func (run *BacktestRun) BuildBacktestResult(acct *Account) {
	if run == nil || acct == nil {
		return
	}

	run.Trades = append(run.Trades[:0], acct.Trades...)

	// build summary result from acct.Trades/account values
}
