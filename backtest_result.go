package trader

// BacktestResult is a lightweight, immutable summary produced at the end of a
// backtest run. All derived fields are computed by Backtest.BuildBacktestResult.
type BacktestResult struct {
	Start        Timestamp
	End          Timestamp
	StartBalance Money // starting account balance
	Balance      Money // final account balance, realised only
	Equity       Money // final equity including any open positions at run end

	Trades int // total non-nil closed trades
	Wins   int // trades with PNL > 0
	Losses int // trades with PNL < 0
	Flat   int // trades with PNL == 0

	// Derived fields populated by BuildBacktestResult.
	NetPL          Money // Balance - StartBalance
	ReturnPct      Rate  // NetPL / StartBalance, RateScale-scaled
	GrossProfit    Money // sum of winning trade PNL
	GrossLoss      Money // sum of losing trade PNL, negative
	WinRate        Rate  // Wins / Trades, RateScale-scaled
	ProfitFactor   Rate  // GrossProfit / abs(GrossLoss), RateScale-scaled
	AvgWinner      Money // average winning trade PNL
	AvgLoser       Money // average losing trade PNL, negative
	RR             Rate  // AvgWinner / abs(AvgLoser), RateScale-scaled
	MaxDrawdown    Money // largest peak-to-trough drop in cumulative PNL, negative
	MaxDrawdownPct Rate  // MaxDrawdown / StartBalance, RateScale-scaled
}
