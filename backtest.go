package trader

import (
	"fmt"
)

// Backtest is the executable form of one backtest run.
// It keeps the immutable request and mutable run-state together so strategies
// can inspect open lots during execution, while the final result is stored in
// the explicit Result field rather than anonymously merged into the run.
type Backtest struct {
	ID        string
	RunConfig RunConfig // resolved config snapshot used for execution

	Request *BacktestRequest
	State   *BacktestRun
	Result  *BacktestResult
}

// CompiledBacktest is the construction-phase output for one backtest run.
// It is immutable and contains the resolved config snapshot plus the validated
// request used to instantiate an executable Backtest later.
type CompiledBacktest struct {
	ID        string
	RunConfig RunConfig
	Request   BacktestRequest
}

// NewRun instantiates a fresh executable Backtest from a compiled definition.
func (c CompiledBacktest) NewRun() Backtest {
	req := c.Request
	return Backtest{
		ID:        c.ID,
		RunConfig: c.RunConfig,
		Request:   &req,
		State:     &BacktestRun{},
	}
}

// CompileBacktests converts a loaded Config into validated, immutable
// backtest definitions. Defaults are applied during construction so execution
// only deals with already-compiled requests.
func CompileBacktests(cfg *Config) ([]CompiledBacktest, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}

	compiled := make([]CompiledBacktest, 0, len(cfg.Runs))
	for _, rawRun := range cfg.Runs {
		runCfg := applyRunSourceDefault(cfg.Defaults, rawRun)
		req, err := compileBacktestRequest(runCfg, cfg.Defaults)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, CompiledBacktest{
			ID:        NewULID(),
			RunConfig: runCfg,
			Request:   *req,
		})
	}
	if len(compiled) == 0 {
		return nil, fmt.Errorf("backtest config must resolve to at least 1 run, got %d", len(compiled))
	}
	return compiled, nil
}

func applyRunSourceDefault(defaults RunDefaults, cfg RunConfig) RunConfig {
	if cfg.Data.Source == "" && defaults.Source != "" {
		cfg.Data.Source = defaults.Source
	}
	return cfg
}

// BacktestRequest holds all the static inputs needed to execute one backtest
// run. It is populated from Config/RunConfig before the run loop starts and
// is not modified during execution.
type BacktestRequest struct {
	Name       string
	ConfigHash string // 8-char SHA256 prefix of execution-affecting config inputs

	StartingBalance Money
	RiskPct         Rate // fraction of equity risked per trade (e.g. 0.005 = 0.5 %)

	DefaultStopPips Pips // fallback stop distance when the strategy doesn't supply one
	DefaultTakePips Pips // fallback take-profit distance
	SlippagePips    Pips // extra adverse fill adjustment applied on every open/close
	MaxSpreadPips   Pips // opens are skipped when the candle spread exceeds this

	Source     string // data source identifier (e.g. "candles", "dukascopy")
	Instrument string // FX pair (e.g. "EUR_USD")
	Strategy
	Exit   ExitStrategy
	Regime RegimeFilter
	TimeRange
}

// compileBacktestRequest builds a validated BacktestRequest from one resolved
// RunConfig and the shared defaults that affect execution semantics.
func compileBacktestRequest(cfg RunConfig, defaults RunDefaults) (*BacktestRequest, error) {
	req, err := compileBacktestComponents(cfg)
	if err != nil {
		return nil, err
	}
	applyBacktestExecutionDefaults(req, cfg, defaults)
	return req, nil
}

// compileBacktestComponents resolves the time range and builds the strategy,
// exit strategy, and regime filter for one backtest run.
func compileBacktestComponents(cfg RunConfig) (*BacktestRequest, error) {
	tr, err := timeRangeFromStrings(cfg.Data.From, cfg.Data.To, cfg.Data.Timeframe)
	if err != nil {
		return nil, fmt.Errorf("build backtest time range for %q: %w", cfg.Name, err)
	}

	strategy, err := GetStrategy(cfg.Strategy)
	if err != nil {
		return nil, fmt.Errorf("build backtest strategy for %q: %w", cfg.Name, err)
	}

	scale := Scale6(PriceScale)
	exit, err := GetExitStrategy(cfg.Exit, scale)
	if err != nil {
		return nil, fmt.Errorf("build exit strategy for %q: %w", cfg.Name, err)
	}

	regime, err := GetRegimeFilter(cfg.Regime, scale)
	if err != nil {
		return nil, fmt.Errorf("build regime filter for %q: %w", cfg.Name, err)
	}

	source := firstNonEmpty(cfg.Data.Source, "candles")
	return &BacktestRequest{
		Name:       cfg.Name,
		Source:     source,
		Instrument: cfg.Data.Instrument,
		Strategy:   strategy,
		Exit:       exit,
		Regime:     regime,
		TimeRange:  tr,
	}, nil
}

// applyBacktestExecutionDefaults injects the defaults that currently affect
// backtest execution semantics onto an already-compiled request.
func applyBacktestExecutionDefaults(req *BacktestRequest, cfg RunConfig, defaults RunDefaults) {
	if req == nil {
		return
	}
	req.ConfigHash = hashBacktestConfig(cfg, defaults)
	req.StartingBalance = MoneyFromFloat(defaults.StartingBalance)
	req.RiskPct = RateFromFloat(defaults.RiskPct / 100.0)
	req.DefaultStopPips = pipsFromFloat(float64(defaults.StopPips))
	req.DefaultTakePips = pipsFromFloat(float64(defaults.TakePips))
	req.SlippagePips = pipsFromFloat(defaults.SlippagePips)
	req.MaxSpreadPips = pipsFromFloat(defaults.MaxSpreadPips)
}

// BuildBacktestResult snapshots the account state into a BacktestResult and
// stores it on the run's explicit Result field. It computes wins/losses/flat
// counts, NetPL, ReturnPct, and WinRate from the account's closed trades.
// Returns nil if run or acct is nil.
func (run *Backtest) BuildBacktestResult(acct *Account) *BacktestResult {
	if run == nil || acct == nil {
		return nil
	}
	if run.Request == nil {
		return nil
	}
	if run.State == nil {
		run.State = &BacktestRun{}
	}

	run.State.Trades = append(run.State.Trades[:0], acct.Trades...)

	res := &BacktestResult{
		Balance: acct.Balance,
		Equity:  acct.Equity,
		Trades:  len(acct.Trades),
		Start:   run.Request.TimeRange.Start,
		End:     run.Request.TimeRange.End,
	}

	for _, tr := range acct.Trades {
		if tr == nil {
			continue
		}
		switch {
		case tr.PNL > 0:
			res.Wins++
		case tr.PNL < 0:
			res.Losses++
		case tr.PNL == 0:
			res.Flat++
		}
	}

	res.NetPL = acct.Balance - run.Request.StartingBalance
	if run.Request.StartingBalance != 0 {
		res.ReturnPct = RateFromFloat(res.NetPL.Float64() / run.Request.StartingBalance.Float64())
	}
	if res.Trades > 0 {
		res.WinRate = RateFromFloat(float64(res.Wins) / float64(res.Trades))
	}
	run.Result = res
	return run.Result

}
