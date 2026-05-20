package trader

type BacktestRun struct {
	Lots   *LotBook
	Trades []*Trade

	// Execution cost tracking — populated by the run loop.
	SpreadFiltered int   // opens suppressed by the max-spread filter
	SpreadOpened   int   // opens that went through (for avg spread calc)
	SpreadSum      Price // sum of candle.AvgSpread at each accepted open
}

func (run *BacktestRun) GetTrades() []*Trade {
	if run == nil {
		return nil
	}
	return run.Trades
}

func (run *BacktestRun) BuildBacktestResult(acct *Account) {
	if run == nil || acct == nil {
		return
	}

	run.Trades = append(run.Trades[:0], acct.Trades...)

	// build summary result from acct.Trades/account values
}
