package trader

import "fmt"

type BacktestRun struct {
	ID string

	*BacktestRequest
	*BacktestResult
	*Positions
	// Trades []Trade
}

func GetBacktestRuns(cfg *Config) ([]BacktestRun, error) {
	runs := make([]BacktestRun, 0, len(cfg.Runs))
	for _, runcfg := range cfg.Runs {
		run := &BacktestRun{
			ID: NewULID(),
		}

		req := newBacktestReq(runcfg)
		if req == nil {
			return nil, fmt.Errorf("failed to create BacktestRequest from config")
		}

		run.BacktestRequest = req
		runs = append(runs, *run)
	}
	if len(runs) < 1 {
		return nil, fmt.Errorf("regression config must resolve to exactly 1 run, got %d", len(runs))
	}
	return runs, nil
}

type BacktestRequest struct {
	Name       string
	Instrument string
	Strict     bool
	Strategy
	TimeRange
}

func newBacktestReq(cfg RunConfig) *BacktestRequest {

	tr, err := timeRangeFromStrings(cfg.Data.From, cfg.Data.To, cfg.Data.Timeframe)
	if err != nil {
		fmt.Printf("Failed to create Backtest Request")
		return nil
	}

	strategy, err := resolveBacktestStrategy(cfg)
	if err != nil {
		return nil
	}

	return &BacktestRequest{
		Name:       cfg.Name,
		Instrument: cfg.Data.Instrument,
		Strategy:   strategy,
		TimeRange:  tr,
	}
}

type BacktestResult struct {
	BacktestRunVars
}

// Result is a lightweight summary of a backtest run.
type BackTestResult struct {
	Balance Money
	Equity  Money

	Trades int
	Wins   int
	Losses int

	Start Timestamp
	End   Timestamp
}

// BacktestRunRow mirrors backtest_runs table.
type BacktestRunVars struct {
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

type ResolvedRun struct {
	Name       string
	Source     string
	Instrument string
	Timeframe  string
	From       string
	To         string
	Strict     bool

	// REMOVE: Accounting should not be part of
	// the back test run
	StartingBalance Money
	AccountCCY      string
	Scale           Scale6
	RR              Rate
	Units           Units

	// REMOVE: This is part of the strategy
	RiskPct  Rate
	StopPips Price
	TakePips Price

	Strategy StrategyConfig
}

func (r ResolvedRun) CandleRequest() (CandleRequest, error) {
	tf, err := parseTimeframe(r.Timeframe)
	if err != nil {
		return CandleRequest{}, err
	}

	start, err := parseDateStart(r.From)
	if err != nil {
		return CandleRequest{}, fmt.Errorf("bad from %q: %w", r.From, err)
	}

	end, err := parseDateEndExclusive(r.To)
	if err != nil {
		return CandleRequest{}, fmt.Errorf("bad to %q: %w", r.To, err)
	}

	return CandleRequest{
		Source:     r.Source,
		Instrument: r.Instrument,
		Timeframe:  tf,
		Range: TimeRange{
			Start: FromTime(start),
			End:   FromTime(end),
		},
		Strict: r.Strict,
	}, nil
}
