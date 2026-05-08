package trader

// Result is a lightweight summary of a backtest run.
type BacktestResult struct {
	Balance Money
	Equity  Money

	Trades int
	Wins   int
	Losses int

	Start Timestamp
	End   Timestamp

	// Derived / computed in Go
	NetPL        Money
	ReturnPct    Rate
	WinRate      Rate
	ProfitFactor Rate
	MaxDDPct     Rate
}
