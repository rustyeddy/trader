package trader

// Result is a lightweight summary of a backtest run.
type Result struct {
	Balance Money
	Equity  Money

	Trades int
	Wins   int
	Losses int

	Start Timestamp
	End   Timestamp
}

// BacktestRunRow mirrors backtest_runs table.
type BacktestRun struct {
	RunID     string
	Name      string
	Kind      string
	Created   Timestamp
	Timeframe string
	Dataset   string

	// Instrument traded in this backtest
	Instrument string
	Strategy   string
	Config     []byte // strategy config

	// Risk Management
	RiskPct  Rate  // 0.005 (0.5%)
	StopPips Price // e.g. 20
	RR       Rate  // take-profit multiple of risk, e.g. 2.0

	// Account and price timeframe
	Start Timestamp
	End   Timestamp

	// Results
	Trades int
	Wins   int
	Losses int

	// account info
	StartBalance Money
	EndBalance   Money

	// Derived / computed in Go
	NetPL        Money
	ReturnPct    Rate
	WinRate      Rate
	ProfitFactor Rate
	MaxDDPct     Rate

	GitCommit string
	OrgPath   string
	EquityPNG string

	Notes       []string
	NextActions []string
}
