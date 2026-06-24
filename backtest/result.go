package backtest

import "github.com/rustyeddy/trader/market"

// BacktestResult is a lightweight, immutable summary produced at the end of a
// backtest run. All derived fields are computed by Backtest.BuildBacktestResult.
type BacktestResult struct {
	Start        market.Timestamp
	End          market.Timestamp
	StartBalance market.Money // starting account balance
	Balance      market.Money // final account balance, realised only
	Equity       market.Money // final equity including any open positions at run end

	Trades int // total non-nil closed trades
	Wins   int // trades with PNL > 0
	Losses int // trades with PNL < 0
	Flat   int // trades with PNL == 0

	// Derived fields populated by BuildBacktestResult.
	NetPL          market.Money // Balance - StartBalance
	ReturnPct      market.Rate  // NetPL / StartBalance, RateScale-scaled
	GrossProfit    market.Money // sum of winning trade PNL
	GrossLoss      market.Money // sum of losing trade PNL, negative
	WinRate        market.Rate  // Wins / Trades, RateScale-scaled
	ProfitFactor   market.Rate  // GrossProfit / abs(GrossLoss), RateScale-scaled
	AvgWinner      market.Money // average winning trade PNL
	AvgLoser       market.Money // average losing trade PNL, negative
	RR             market.Rate  // AvgWinner / abs(AvgLoser), RateScale-scaled
	MaxDrawdown    market.Money // largest peak-to-trough drop in cumulative PNL, negative
	MaxDrawdownPct market.Rate  // MaxDrawdown / StartBalance, RateScale-scaled
}
