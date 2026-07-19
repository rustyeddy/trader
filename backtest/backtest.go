package backtest

import (
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
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

// emptyLotBook is returned by OpenLots when no lot state exists, so callers can
// always safely call Len/Range without a nil check.
var emptyLotBook = &account.LotBook{}

// Instrument implements StrategyContext: the instrument being traded.
func (b *Backtest) Instrument() string {
	if b == nil || b.Request == nil {
		return ""
	}
	return b.Request.Instrument
}

// OpenLots implements StrategyContext: a read-only view of the open lots. It
// never returns nil.
func (b *Backtest) OpenLots() strategy.LotView {
	if b == nil || b.State == nil || b.State.Lots == nil {
		return emptyLotBook
	}
	return b.State.Lots
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
		runCfg := rawRun
		if runCfg.Data.Source == "" && cfg.Defaults.Source != "" {
			runCfg.Data.Source = cfg.Defaults.Source
		}
		req, err := compileBacktestComponents(runCfg)
		if err != nil {
			return nil, err
		}
		applyBacktestExecutionDefaults(req, runCfg, cfg.Defaults)
		compiled = append(compiled, CompiledBacktest{
			ID:        idgen.NewULID(),
			RunConfig: runCfg,
			Request:   *req,
		})
	}
	if len(compiled) == 0 {
		return nil, fmt.Errorf("backtest config must resolve to at least 1 run, got %d", len(compiled))
	}
	return compiled, nil
}

// BacktestRequest holds all the static inputs needed to execute one backtest
// run. It is populated from Config/RunConfig before the run loop starts and
// is not modified during execution.
type BacktestRequest struct {
	Name       string
	ConfigHash string // 8-char SHA256 prefix of execution-affecting config inputs

	StartingBalance types.Money
	RiskPct         types.Rate // fraction of equity risked per trade (e.g. 0.005 = 0.5 %)

	DefaultStopPips types.Pips // fallback stop distance when the strategy doesn't supply one
	DefaultTakePips types.Pips // fallback take-profit distance
	SlippagePips    types.Pips // extra adverse fill adjustment applied on every open/close
	MaxSpreadPips   types.Pips // opens are skipped when the candle spread exceeds this

	Source     string // data source identifier (e.g. "candles", "dukascopy")
	Instrument string // FX pair (e.g. "EUR_USD")
	Strategy   strategy.Strategy
	Exit       strategy.ExitStrategy
	Regime     strategy.RegimeFilter
	TimeRange  types.TimeRange
}

// compileBacktestComponents resolves the time range and builds the strategy,
// exit strategy, and regime filter for one backtest run.
func compileBacktestComponents(cfg RunConfig) (*BacktestRequest, error) {
	tr, err := types.TimeRangeFromStrings(cfg.Data.From, cfg.Data.To, cfg.Data.Timeframe)
	if err != nil {
		return nil, fmt.Errorf("build backtest time range for %q: %w", cfg.Name, err)
	}

	strat, err := strategy.GetStrategy(cfg.Strategy)
	if err != nil {
		return nil, fmt.Errorf("build backtest strategy for %q: %w", cfg.Name, err)
	}

	scale := types.Scale6(types.PriceScale)
	exit, err := strategy.GetExitStrategy(cfg.Exit, scale)
	if err != nil {
		return nil, fmt.Errorf("build exit strategy for %q: %w", cfg.Name, err)
	}

	regime, err := strategy.GetRegimeFilter(cfg.Regime, scale)
	if err != nil {
		return nil, fmt.Errorf("build regime filter for %q: %w", cfg.Name, err)
	}

	source := firstNonEmpty(cfg.Data.Source, "candles")
	return &BacktestRequest{
		Name:       cfg.Name,
		Source:     source,
		Instrument: cfg.Data.Instrument,
		Strategy:   strat,
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
	req.StartingBalance = types.MoneyFromFloat(defaults.StartingBalance)
	req.RiskPct = types.RateFromFloat(defaults.RiskPct / 100.0)
	req.DefaultStopPips = types.PipsFromFloat(float64(defaults.StopPips))
	req.DefaultTakePips = types.PipsFromFloat(float64(defaults.TakePips))
	req.SlippagePips = types.PipsFromFloat(defaults.SlippagePips)
	req.MaxSpreadPips = types.PipsFromFloat(defaults.MaxSpreadPips)
}

// BuildBacktestResult snapshots the account state into a BacktestResult and
// stores it on the run's explicit Result field. It computes trade counts,
// returns, gross P/L, averages, risk/reward, and closed-trade drawdown from
// the account's closed trades.
// Returns nil if run or acct is nil.
func (run *Backtest) BuildBacktestResult(acct *account.Account) *BacktestResult {
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
		Start:        run.Request.TimeRange.Start,
		End:          run.Request.TimeRange.End,
		StartBalance: run.Request.StartingBalance,
		Balance:      acct.Balance,
		Equity:       acct.Equity,
	}

	var running, peak types.Money
	for _, tr := range acct.Trades {
		if tr == nil {
			continue
		}
		res.Trades++
		running += tr.PNL
		if running > peak {
			peak = running
		}
		if drop := peak - running; drop > -res.MaxDrawdown {
			res.MaxDrawdown = -drop
		}

		switch {
		case tr.PNL > 0:
			res.Wins++
			res.GrossProfit += tr.PNL
		case tr.PNL < 0:
			res.Losses++
			res.GrossLoss += tr.PNL
		default:
			res.Flat++
		}
	}

	res.NetPL = acct.Balance - res.StartBalance
	if res.StartBalance != 0 {
		res.ReturnPct = types.RateFromFloat(res.NetPL.Float64() / res.StartBalance.Float64())
		res.MaxDrawdownPct = types.RateFromFloat(res.MaxDrawdown.Float64() / res.StartBalance.Float64())
	}
	if res.Trades > 0 {
		res.WinRate = types.RateFromFloat(float64(res.Wins) / float64(res.Trades))
	}
	if res.Wins > 0 {
		res.AvgWinner = types.Money(int64(res.GrossProfit) / int64(res.Wins))
	}
	if res.Losses > 0 {
		res.AvgLoser = types.Money(int64(res.GrossLoss) / int64(res.Losses))
	}
	if res.GrossLoss < 0 {
		res.ProfitFactor = types.RateFromFloat(res.GrossProfit.Float64() / -res.GrossLoss.Float64())
	}
	if res.AvgLoser < 0 {
		res.RR = types.RateFromFloat(res.AvgWinner.Float64() / -res.AvgLoser.Float64())
	}

	run.Result = res
	return run.Result
}
