package backtest

import "github.com/rustyeddy/trader/types"

// BacktestResult is a lightweight, immutable summary produced at the end of a
// backtest run. All derived fields are computed by Backtest.BuildBacktestResult.
type BacktestResult struct {
	Start        types.Timestamp
	End          types.Timestamp
	StartBalance types.Money // starting account balance
	Balance      types.Money // final account balance, realised only
	Equity       types.Money // final equity including any open positions at run end

	Trades int // total non-nil closed trades
	Wins   int // trades with PNL > 0
	Losses int // trades with PNL < 0
	Flat   int // trades with PNL == 0

	// Derived fields populated by BuildBacktestResult.
	NetPL          types.Money // Balance - StartBalance
	ReturnPct      types.Rate  // NetPL / StartBalance, RateScale-scaled
	GrossProfit    types.Money // sum of winning trade PNL
	GrossLoss      types.Money // sum of losing trade PNL, negative
	WinRate        types.Rate  // Wins / Trades, RateScale-scaled
	ProfitFactor   types.Rate  // GrossProfit / abs(GrossLoss), RateScale-scaled
	AvgWinner      types.Money // average winning trade PNL
	AvgLoser       types.Money // average losing trade PNL, negative
	RR             types.Rate  // AvgWinner / abs(AvgLoser), RateScale-scaled
	MaxDrawdown    types.Money // largest peak-to-trough drop in cumulative PNL, negative
	MaxDrawdownPct types.Rate  // MaxDrawdown / StartBalance, RateScale-scaled
}
