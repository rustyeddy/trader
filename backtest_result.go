package trader

// BacktestResult is a lightweight, immutable summary produced at the end of a
// backtest run. All derived fields are computed by Backtest.BuildBacktestResult.
type BacktestResult struct {
	Balance Money // final account balance (realised only)
	Equity  Money // final equity including any open positions at run end

	Trades int // total closed trades
	Wins   int // trades with PNL > 0
	Losses int // trades with PNL < 0
	Flat   int // trades with PNL == 0

	Start Timestamp
	End   Timestamp

	// Derived fields — populated by BuildBacktestResult.
	NetPL        Money // Balance − StartingBalance
	ReturnPct    Rate  // NetPL / StartingBalance, RateScale-scaled
	WinRate      Rate  // Wins / Trades, RateScale-scaled
	ProfitFactor Rate  // gross wins / gross losses (not yet implemented)
	MaxDDPct     Rate  // maximum peak-to-trough drawdown % (not yet implemented)
}
