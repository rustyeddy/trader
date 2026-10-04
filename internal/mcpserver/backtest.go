package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rustyeddy/trader/internal/report"
)

// The backtest tools (#437): trader_run_backtest runs a backtest and
// returns a compact summary; trader_backtest_result returns a stored
// run's full report. Both are thin adapters over the Backtests
// capability, which the composition root implements over the same
// backtest composition `trader backtest run` uses (backtestcfg) and the
// shared run store (`trader backtest show` reads the same records).

var (
	// ErrBacktestsUnavailable reports a backtest tool called on a server
	// constructed without a Backtests capability.
	ErrBacktestsUnavailable = errors.New("backtests are not configured on this server")
	// ErrInvalidBacktest marks a backtest request rejected for its input
	// (configuration, symbols, values, or run ID); its message describes
	// only what was asked for.
	ErrInvalidBacktest = errors.New("invalid backtest request")
	// ErrBacktestDataNotReady reports a backtest whose canonical market
	// data is not current, on a server that may not build it (no write
	// access).
	ErrBacktestDataNotReady = errors.New("market data is not ready for this backtest; run trader_marketdata_canonicalize (needs --allow-writes)")
)

// Backtests is the backtest capability the tools consume, defined here
// by its consumer (ADR-070) and implemented at the composition root.
type Backtests interface {
	// Run runs one backtest and persists its report, calling progress as
	// it enters each stage. Errors for bad input wrap ErrInvalidBacktest;
	// data that is not ready wraps ErrBacktestDataNotReady.
	Run(ctx context.Context, req BacktestInput, progress func(stage string)) (report.BacktestReport, error)
	// Result returns the stored report for runID.
	Result(ctx context.Context, runID string) (report.BacktestReport, error)
}

// BacktestInput is trader_run_backtest's request: the inputs `trader
// backtest run` accepts for an in-process strategy. Omitted fields take
// the CLI's defaults. External strategy executables and journal files
// are not available over MCP.
type BacktestInput struct {
	Symbols            []string         `json:"symbols" jsonschema:"symbols to backtest, for example EURUSD; ema-cross and quantity mode take exactly one"`
	Provider           string           `json:"provider,omitempty" jsonschema:"market-data provider (oanda, alpaca, or stooq); defaults to the server's provider"`
	Interval           string           `json:"interval,omitempty" jsonschema:"bar interval: M1, H1, H4, D1, or W1 (default H1)"`
	From               string           `json:"from" jsonschema:"range start, YYYY-MM-DD or RFC3339"`
	To                 string           `json:"to" jsonschema:"range end (exclusive), YYYY-MM-DD or RFC3339"`
	AdverseDistance    string           `json:"adverse_distance" jsonschema:"adverse price distance used for position sizing, for example 0.01"`
	StartingCapital    string           `json:"starting_capital,omitempty" jsonschema:"starting account cash (default 10000)"`
	Currency           string           `json:"currency,omitempty" jsonschema:"account currency (default USD)"`
	RiskFraction       string           `json:"risk_fraction,omitempty" jsonschema:"fraction of equity to risk per trade (default 0.01)"`
	InitialMarginRatio string           `json:"initial_margin_ratio,omitempty" jsonschema:"equity required per unit of gross notional: 1 is unlevered (default), 0.5 permits 2x"`
	WarmupBars         int              `json:"warmup_bars,omitempty" jsonschema:"warm-up bars before the buy-and-hold demo may trade"`
	Strategy           BacktestStrategy `json:"strategy,omitempty"`
}

// BacktestStrategy selects and configures the in-process strategy.
type BacktestStrategy struct {
	Name        string `json:"name,omitempty" jsonschema:"buy-and-hold (default) or ema-cross"`
	// FastPeriod and SlowPeriod are pointers so an explicit 0 (which
	// fails validation, as the CLI's --fast-period 0 does) stays distinct
	// from omission (the default).
	FastPeriod  *int   `json:"fast_period,omitempty" jsonschema:"ema-cross fast period (default 20)"`
	SlowPeriod  *int   `json:"slow_period,omitempty" jsonschema:"ema-cross slow period (default 50)"`
	AllowedSide string `json:"allowed_side,omitempty" jsonschema:"ema-cross direction: both (default), long-only, or short-only"`
	Quantity    string `json:"quantity,omitempty" jsonschema:"buy-and-hold quantity mode: buy exactly this quantity of the single symbol"`
	BuyDate     string `json:"buy_date,omitempty" jsonschema:"quantity mode: buy on the first bar at or after this date"`
	SellDate    string `json:"sell_date,omitempty" jsonschema:"quantity mode: exit on the first bar at or after this date"`
}

// RunBacktestOutput is trader_run_backtest's result: the run's identity
// and the report's summary sections — run, dataset, performance, trade
// statistics, margin, and account — plus trade counts. The full report
// (trades and equity curve) is available from trader_backtest_result.
type RunBacktestOutput struct {
	RunID        string         `json:"run_id"`
	ConfigDigest string         `json:"config_digest"`
	Summary      map[string]any `json:"summary"`
}

// BacktestResultInput is trader_backtest_result's request.
type BacktestResultInput struct {
	RunID string `json:"run_id" jsonschema:"the run ID trader_run_backtest (or trader backtest run) returned"`
}

// BacktestResultOutput is trader_backtest_result's result: the stored
// report, as `trader backtest show --format json` renders it — except
// that a run made with an external strategy executable (the CLI's
// --strategy-exec) has its launch details removed from
// run.strategy_parameters: the executable and config paths and the
// argument list, which are machine-local (ADR-068). The strategy's
// identity and the executable and config content digests remain.
type BacktestResultOutput struct {
	RunID  string         `json:"run_id"`
	Report map[string]any `json:"report"`
}

// summary is the projection trader_run_backtest returns: the report's
// summary sections only, so a long run's equity curve and trade lists are
// never encoded for it.
type summary struct {
	Run         report.RunInfo         `json:"run"`
	Dataset     []report.DatasetReport `json:"dataset"`
	Performance report.Performance     `json:"performance"`
	TradeStats  report.TradeStats      `json:"trade_stats"`
	Margin      report.MarginReport    `json:"margin"`
	Account     report.AccountReport   `json:"account"`
}

// externalLaunchDetails are the machine-local keys an external
// strategy's recorded parameters carry (backtestcfg's
// externalStrategyParams); MCP never returns them.
var externalLaunchDetails = []string{"exec", "config", "args"}

func (s *server) registerBacktests(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "trader_run_backtest",
		Description: "Run a backtest with an in-process strategy (buy-and-hold or ema-cross) over canonical market data, as " +
			"`trader backtest run` does, and return its run ID, config digest, and summary. The run is saved for " +
			"trader_backtest_result. It runs synchronously, with a progress notification per stage; canonical data is " +
			"built first only on a server with --allow-writes, otherwise it must already be current.",
	}, s.runBacktest)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "trader_backtest_result",
		Description: "Return a completed backtest's full stored report by run ID, the same record `trader backtest show` reads.",
	}, s.backtestResult)
}

func (s *server) runBacktest(ctx context.Context, req *mcp.CallToolRequest, in BacktestInput) (*mcp.CallToolResult, RunBacktestOutput, error) {
	const tool, what = "trader_run_backtest", "backtest"
	if s.deps.Backtests == nil {
		return nil, RunBacktestOutput{}, ErrBacktestsUnavailable
	}
	notify := s.stageProgress(ctx, req, 3)
	rep, err := s.deps.Backtests.Run(ctx, in, notify)
	if err != nil {
		return nil, RunBacktestOutput{}, s.publicError(ctx, tool, what, err)
	}
	notify("saved")
	out := RunBacktestOutput{RunID: rep.Run.RunID, ConfigDigest: rep.Run.ConfigDigest}
	rep.Run = withoutLaunchDetails(rep.Run)
	if out.Summary, err = asObject(summary{
		Run: rep.Run, Dataset: rep.Dataset, Performance: rep.Performance,
		TradeStats: rep.TradeStats, Margin: rep.Margin, Account: rep.Account,
	}); err != nil {
		return nil, RunBacktestOutput{}, s.publicError(ctx, tool, what, err)
	}
	out.Summary["closed_trade_count"] = len(rep.ClosedTrades)
	out.Summary["open_trade_count"] = len(rep.OpenTrades)
	return nil, out, nil
}

func (s *server) backtestResult(ctx context.Context, _ *mcp.CallToolRequest, in BacktestResultInput) (*mcp.CallToolResult, BacktestResultOutput, error) {
	const tool, what = "trader_backtest_result", "backtest result"
	if s.deps.Backtests == nil {
		return nil, BacktestResultOutput{}, ErrBacktestsUnavailable
	}
	rep, err := s.deps.Backtests.Result(ctx, in.RunID)
	if err != nil {
		return nil, BacktestResultOutput{}, s.publicError(ctx, tool, what, err)
	}
	rep.Run = withoutLaunchDetails(rep.Run)
	full, err := asObject(rep)
	if err != nil {
		return nil, BacktestResultOutput{}, s.publicError(ctx, tool, what, err)
	}
	return nil, BacktestResultOutput{RunID: rep.Run.RunID, Report: full}, nil
}

// asObject returns v as its JSON object. Going through the report's own
// JSON encoding keeps tool output identical to `trader backtest show
// --format json`, exact numeric values included.
func asObject(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding report: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decoding report: %w", err)
	}
	return out, nil
}

// withoutLaunchDetails removes an external strategy's machine-local
// launch details (externalLaunchDetails) from run's recorded strategy
// parameters. Any other strategy's parameters are returned unchanged.
func withoutLaunchDetails(run report.RunInfo) report.RunInfo {
	var params map[string]any
	if len(run.StrategyParameters) == 0 || json.Unmarshal(run.StrategyParameters, &params) != nil || params["mode"] != "external" {
		return run
	}
	for _, k := range externalLaunchDetails {
		delete(params, k)
	}
	b, err := json.Marshal(params)
	if err != nil {
		run.StrategyParameters = nil
		return run
	}
	run.StrategyParameters = b
	return run
}

// stageProgress returns a callback that sends a progress notification per
// stage (out of total), or a no-op when the client asked for no progress.
func (s *server) stageProgress(ctx context.Context, req *mcp.CallToolRequest, total int) func(stage string) {
	if req == nil || req.Params == nil || req.Session == nil || req.Params.GetProgressToken() == nil {
		return func(string) {}
	}
	token, done := req.Params.GetProgressToken(), 0
	return func(stage string) {
		done++
		err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token, Progress: float64(done), Total: float64(total), Message: stage,
		})
		if err != nil {
			s.deps.Logger.DebugContext(ctx, "mcp progress notification failed", "error", err)
		}
	}
}
