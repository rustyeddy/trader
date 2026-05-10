package trader

type BacktestRun struct {
	*Positions
	Trades []Trade
}
