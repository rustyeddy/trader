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
