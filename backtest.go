package trader

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// Backtest is the top-level unit of work for a single backtesting run.
// It composes a request (what to run), a mutable run-state (open lots,
// execution cost counters), and an immutable result (produced at the end).
// RunConfig is the original config snapshot; it is carried through to the
// summary so every report is self-describing.
type Backtest struct {
	ID        string
	RunConfig RunConfig // original config snapshot before transformation

	*BacktestRequest
	*BacktestRun
	*BacktestResult
}

// GetBacktests converts a loaded Config into a slice of ready-to-run Backtest
// values. Defaults from cfg.Defaults (balance, risk, stop/take pips, slippage,
// max spread) are merged into each run. Returns an error if the config
// resolves to zero runs or any run is misconfigured.
func GetBacktests(cfg *Config) ([]Backtest, error) {
	runs := make([]Backtest, 0, len(cfg.Runs))
	for _, runcfg := range cfg.Runs {
		run := &Backtest{
			ID:          NewULID(),
			RunConfig:   runcfg,
			BacktestRun: &BacktestRun{},
		}

		// Cascade defaults.source into per-run data.source when not overridden.
		if runcfg.Data.Source == "" && cfg.Defaults.Source != "" {
			runcfg.Data.Source = cfg.Defaults.Source
		}

		req := newBacktestReq(runcfg)
		if req == nil {
			return nil, fmt.Errorf("failed to create BacktestRequest from config")
		}

		req.StartingBalance = MoneyFromFloat(cfg.Defaults.StartingBalance)
		req.RiskPct = RateFromFloat(cfg.Defaults.RiskPct / 100.0)
		req.DefaultStopPips = pipsFromFloat(float64(cfg.Defaults.StopPips))
		req.DefaultTakePips = pipsFromFloat(float64(cfg.Defaults.TakePips))
		req.SlippagePips = pipsFromFloat(cfg.Defaults.SlippagePips)
		req.MaxSpreadPips = pipsFromFloat(cfg.Defaults.MaxSpreadPips)
		req.ConfigHash = hashRunConfig(runcfg)

		run.BacktestRequest = req
		runs = append(runs, *run)
	}
	if len(runs) < 1 {
		return nil, fmt.Errorf("regression config must resolve to exactly 1 run, got %d", len(runs))
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

// newBacktestReq builds a BacktestRequest from a single RunConfig, resolving
// the time range, strategy, exit strategy, and regime filter. Returns nil and
// logs to stdout on any construction error (callers must nil-check).
func newBacktestReq(cfg RunConfig) *BacktestRequest {

	tr, err := timeRangeFromStrings(cfg.Data.From, cfg.Data.To, cfg.Data.Timeframe)
	if err != nil {
		fmt.Printf("Failed to create Backtest Request")
		return nil
	}

	strategy, err := GetStrategy(cfg.Strategy)
	if err != nil {
		return nil
	}

	scale := Scale6(PriceScale)
	exit, err := GetExitStrategy(cfg.Exit, scale)
	if err != nil {
		fmt.Printf("failed to build exit strategy: %v\n", err)
		return nil
	}

	regime, err := GetRegimeFilter(cfg.Regime, scale)
	if err != nil {
		fmt.Printf("failed to build regime filter: %v\n", err)
		return nil
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
	}
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
// stores it on the run. It computes wins/losses/flat counts, NetPL, ReturnPct,
// and WinRate from the account's closed trades. Returns nil if run or acct is nil.
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
	run.BacktestResult = res
	return run.BacktestResult

}

// Summary builds a fully-populated BacktestReportSummary from the run's
// request and result fields. It is safe to call after BuildBacktestResult.
// Returns a zero-value summary if any required field is nil.
func (run *Backtest) Summary() BacktestReportSummary {
	if run == nil || run.BacktestRequest == nil || run.BacktestResult == nil || run.Strategy == nil {
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

		Trades:         run.BacktestResult.Trades,
		Wins:           run.BacktestResult.Wins,
		Losses:         run.BacktestResult.Losses,
		StartBalance:   run.StartingBalance.Float64(),
		EndBalance:     run.BacktestResult.Balance.Float64(),
		NetPL:          run.BacktestResult.NetPL.Float64(),
		ReturnPct:      run.BacktestResult.ReturnPct.Float64() * 100,
		WinRate:        run.BacktestResult.WinRate.Float64() * 100,
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
