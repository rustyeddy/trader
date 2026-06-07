package trader

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// Backtest is the executable form of one backtest run.
// It keeps the immutable request and mutable run-state together so strategies
// can inspect open lots during execution, while the final result is stored in
// the explicit Result field rather than anonymously merged into the run.
type Backtest struct {
	ID        string
	RunConfig RunConfig // resolved config snapshot used for execution

	*BacktestRequest
	*BacktestRun
	Result *BacktestResult
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
		ID:              c.ID,
		RunConfig:       c.RunConfig,
		BacktestRequest: &req,
		BacktestRun:     &BacktestRun{},
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
		runCfg := applyRunDefaults(cfg.Defaults, rawRun)
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

// GetBacktests adapts compiled definitions into executable Backtest values.
func GetBacktests(cfg *Config) ([]Backtest, error) {
	compiled, err := CompileBacktests(cfg)
	if err != nil {
		return nil, err
	}

	runs := make([]Backtest, 0, len(compiled))
	for _, def := range compiled {
		runs = append(runs, def.NewRun())
	}
	return runs, nil
}

// hashRunConfig returns the first 8 hex characters of the SHA256 of the
// run's data/strategy/exit/regime params. The Name field is excluded because
// it is a label, not a parameter that affects results. This hash is used as
// a stable filename suffix: same params → same hash → same file on disk.
func hashRunConfig(cfg RunConfig) string {
	type hashable struct {
		Data     DataConfig     `json:"data"`
		Strategy StrategyConfig `json:"strategy"`
		Exit     ExitConfig     `json:"exit"`
		Regime   RegimeConfig   `json:"regime"`
	}
	b, _ := json.Marshal(hashable{
		Data:     cfg.Data,
		Strategy: cfg.Strategy,
		Exit:     cfg.Exit,
		Regime:   cfg.Regime,
	})
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:4]) // 8 hex chars
}

func applyRunDefaults(defaults RunDefaults, cfg RunConfig) RunConfig {
	if cfg.Data.Source == "" && defaults.Source != "" {
		cfg.Data.Source = defaults.Source
	}
	return cfg
}

// compileBacktestRequest builds a validated BacktestRequest from one resolved
// RunConfig and the shared defaults that affect execution semantics.
func compileBacktestRequest(cfg RunConfig, defaults RunDefaults) (*BacktestRequest, error) {
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
		Name:            cfg.Name,
		ConfigHash:      hashRunConfig(cfg),
		StartingBalance: MoneyFromFloat(defaults.StartingBalance),
		RiskPct:         RateFromFloat(defaults.RiskPct / 100.0),
		DefaultStopPips: pipsFromFloat(float64(defaults.StopPips)),
		DefaultTakePips: pipsFromFloat(float64(defaults.TakePips)),
		SlippagePips:    pipsFromFloat(defaults.SlippagePips),
		MaxSpreadPips:   pipsFromFloat(defaults.MaxSpreadPips),
		Source:          source,
		Instrument:      cfg.Data.Instrument,
		Strategy:        strategy,
		Exit:            exit,
		Regime:          regime,
		TimeRange:       tr,
	}, nil
}

// BacktestRequest holds all the static inputs needed to execute one backtest
// run. It is populated from Config/RunConfig before the run loop starts and
// is not modified during execution.
type BacktestRequest struct {
	Name       string
	ConfigHash string // 8-char SHA256 prefix of the RunConfig params (set by GetBacktests)

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

// BuildBacktestResult snapshots the account state into a BacktestResult and
// stores it on the run's explicit Result field. It computes wins/losses/flat
// counts, NetPL, ReturnPct, and WinRate from the account's closed trades.
// Returns nil if run or acct is nil.
func (run *Backtest) BuildBacktestResult(acct *Account) *BacktestResult {
	if run == nil || acct == nil {
		return nil
	}
	if run.BacktestRun == nil {
		run.BacktestRun = &BacktestRun{}
	}

	run.BacktestRun.Trades = append(run.BacktestRun.Trades[:0], acct.Trades...)

	res := &BacktestResult{
		Balance: acct.Balance,
		Equity:  acct.Equity,
		Trades:  len(acct.Trades),
		Start:   run.TimeRange.Start,
		End:     run.TimeRange.End,
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

	res.NetPL = acct.Balance - run.StartingBalance
	if run.StartingBalance != 0 {
		res.ReturnPct = RateFromFloat(res.NetPL.Float64() / run.StartingBalance.Float64())
	}
	if res.Trades > 0 {
		res.WinRate = RateFromFloat(float64(res.Wins) / float64(res.Trades))
	}
	run.Result = res
	return run.Result

}

// Summary builds a fully-populated BacktestReportSummary from the run's
// request and result fields. It is safe to call after BuildBacktestResult.
// Returns a zero-value summary if any required field is nil.
func (run *Backtest) Summary() BacktestReportSummary {
	if run == nil || run.BacktestRequest == nil || run.Result == nil || run.Strategy == nil {
		return BacktestReportSummary{}
	}

	var trades []BacktestReportTrade
	for _, tr := range run.BacktestRun.GetTrades() {
		if tr == nil {
			continue
		}

		trades = append(trades, BacktestReportTrade{
			ID:              tr.ID,
			Instrument:      tr.Instrument,
			Side:            tr.Side.String(),
			Units:           int64(tr.Units),
			OpenPrice:       tr.EntryPrice.Float64(),
			ClosePrice:      tr.ExitPrice.Float64(),
			OpenTime:        formatBacktestSummaryTime(tr.EntryTime),
			CloseTime:       formatBacktestSummaryTime(tr.ExitTime),
			PNL:             tr.PNL.Float64(),
			StopPrice:       tr.Stop.Float64(),
			TakeProfitPrice: tr.Take.Float64(),
		})
	}

	maxDD, avgWinner, avgLoser := computeTradeStats(trades)
	rr := 0.0
	if avgLoser != 0 {
		rr = avgWinner / -avgLoser
	}

	avgSpreadPips, spreadFiltered := executionCostStats(run)

	return BacktestReportSummary{
		Name:       run.Name,
		Strategy:   run.Strategy.Name(),
		Instrument: run.Instrument,
		Timeframe:  run.TimeRange.TF.String(),
		Start:      formatBacktestSummaryTime(run.TimeRange.Start),
		End:        formatBacktestSummaryTime(run.TimeRange.End),

		Trades:         run.Result.Trades,
		Wins:           run.Result.Wins,
		Losses:         run.Result.Losses,
		StartBalance:   run.StartingBalance.Float64(),
		EndBalance:     run.Result.Balance.Float64(),
		NetPL:          run.Result.NetPL.Float64(),
		ReturnPct:      run.Result.ReturnPct.Float64() * 100,
		WinRate:        run.Result.WinRate.Float64() * 100,
		RiskPct:        run.RiskPct.Float64() * 100,
		Stop:           stopDescription(run),
		Regime:         regimeDescription(run),
		MaxSpread:      maxSpreadDescription(run),
		Slippage:       slippageDescription(run),
		AvgSpreadPips:  avgSpreadPips,
		SpreadFiltered: spreadFiltered,
		MaxDrawdown:    maxDD,
		AvgWinner:      avgWinner,
		AvgLoser:       avgLoser,
		RR:             rr,

		TradeDetails: trades,

		ConfigHash:  run.ConfigHash,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Config:      run.RunConfig,
	}
}

// computeTradeStats derives max drawdown, avg winner, and avg loser from the trade list.
// MaxDrawdown is the largest peak-to-trough drop in cumulative P/L (returned as negative).
func computeTradeStats(trades []BacktestReportTrade) (maxDrawdown, avgWinner, avgLoser float64) {
	var running, peak float64
	var winSum, lossSum float64
	var winN, lossN int

	for _, tr := range trades {
		running += tr.PNL
		if running > peak {
			peak = running
		}
		if drop := peak - running; drop > -maxDrawdown {
			maxDrawdown = -drop
		}
		if tr.PNL > 0 {
			winSum += tr.PNL
			winN++
		} else if tr.PNL < 0 {
			lossSum += tr.PNL
			lossN++
		}
	}

	if winN > 0 {
		avgWinner = winSum / float64(winN)
	}
	if lossN > 0 {
		avgLoser = lossSum / float64(lossN)
	}
	return
}

// regimeDescription returns the regime filter's name for display in the
// summary, or an empty string when no filter is configured.
func regimeDescription(run *Backtest) string {
	if run.Regime != nil {
		if name := run.Regime.Name(); name != "" {
			return name
		}
	}
	return ""
}

// slippageDescription returns a formatted slippage label (e.g. "1.5p") or
// an empty string when slippage is zero.
func slippageDescription(run *Backtest) string {
	if run.SlippagePips == 0 {
		return ""
	}
	return fmt.Sprintf("%.1fp", run.SlippagePips.Float64())
}

// executionCostStats returns the average spread (in pips) across accepted
// opens and the number of opens that were suppressed by the max-spread filter.
func executionCostStats(run *Backtest) (avgSpreadPips float64, spreadFiltered int) {
	if run.BacktestRun == nil {
		return 0, 0
	}
	spreadFiltered = run.BacktestRun.SpreadFiltered
	if run.BacktestRun.SpreadOpened == 0 {
		return 0, spreadFiltered
	}
	inst := GetInstrument(run.Instrument)
	if inst == nil {
		return 0, spreadFiltered
	}
	unitsPerPip := float64(inst.PriceUnitsPerPip())
	avgSpreadPips = float64(run.SpreadSum) / float64(run.SpreadOpened) / unitsPerPip
	return avgSpreadPips, spreadFiltered
}

// maxSpreadDescription returns a formatted max-spread label (e.g. "2.0p") or
// an empty string when no spread filter is configured.
func maxSpreadDescription(run *Backtest) string {
	if run.MaxSpreadPips == 0 {
		return ""
	}
	return fmt.Sprintf("%.1fp", run.MaxSpreadPips.Float64())
}

// stopDescription returns the stop label for the summary, preferring the exit
// strategy's name when one is configured, then falling back to the entry
// strategy's StopDescription.
func stopDescription(run *Backtest) string {
	if run.Exit != nil {
		if name := run.Exit.Name(); name != "" {
			return name
		}
	}
	return run.Strategy.StopDescription()
}
