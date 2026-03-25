package backtest

import (
	"github.com/rustyeddy/trader/types"
)

// BacktestRunRow mirrors backtest_runs table.
type BacktestRun struct {
	RunID     string
	Name      string
	Kind      string
	Created   types.Timestamp
	Timeframe string
	Dataset   string

	// Instrument traded in this backtest
	Instrument string
	Strategy   string
	Config     []byte // strategy config

	// Risk Management
	RiskPct  types.Rate  // 0.005 (0.5%)
	StopPips types.Price // e.g. 20
	RR       types.Rate  // take-profit multiple of risk, e.g. 2.0

	// Account and price timeframe
	Start types.Timestamp
	End   types.Timestamp

	// Results
	Trades int
	Wins   int
	Losses int

	// account info
	StartBalance types.Money
	EndBalance   types.Money

	// Derived / computed in Go
	NetPL        types.Money
	ReturnPct    types.Rate
	WinRate      types.Rate
	ProfitFactor types.Rate
	MaxDDPct     types.Rate

	GitCommit string
	OrgPath   string
	EquityPNG string

	Notes       []string
	NextActions []string
}
