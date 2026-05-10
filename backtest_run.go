package trader

type BacktestRun struct {
	*Positions
	Trades []*Trade
}

func (run *BacktestRun) BuildBacktestResult(acct *Account) {
	if run == nil || acct == nil {
		return
	}

	run.Trades = append(run.Trades[:0], acct.Trades...)

	// build summary result from acct.Trades/account values
}
