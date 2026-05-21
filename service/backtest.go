package service

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
)

// RunBacktest executes a single configured *Backtest end-to-end and
// returns the rendered summary. The Trader/Broker/Account are wired up
// fresh per call; service does not retain backtest state.
func (s *Service) RunBacktest(ctx context.Context, run *trader.Backtest) (trader.BacktestReportSummary, error) {
	if run == nil || run.BacktestRequest == nil {
		return trader.BacktestReportSummary{}, fmt.Errorf("nil Backtest")
	}

	t := &trader.Trader{
		DataManager: trader.GetDataManager(),
	}
	t.Broker = trader.NewBroker("sim")
	acct := trader.NewAccount("backtest", run.StartingBalance)
	if run.RiskPct != 0 {
		acct.RiskPct = run.RiskPct
	}
	t.Broker.Account = acct

	if err := t.Backtest(ctx, run); err != nil {
		return trader.BacktestReportSummary{}, fmt.Errorf("backtest %q: %w", run.Name, err)
	}
	return run.Summary(), nil
}

// RunBacktestConfigs loads a slice of YAML config files, expands each
// into per-run *Backtest objects, and executes them all. Returns the
// summaries in submission order along with any per-run errors (errors
// are non-fatal: one bad run doesn't abort the others).
//
// This is the typical "regression sweep" entry point used by both the
// CLI and the future REST endpoint.
func (s *Service) RunBacktestConfigs(ctx context.Context, configPaths []string) ([]trader.BacktestReportSummary, error) {
	var summaries []trader.BacktestReportSummary

	for _, cfgPath := range configPaths {
		cfg, err := trader.LoadConfig(cfgPath)
		if err != nil {
			return summaries, fmt.Errorf("load config %q: %w", cfgPath, err)
		}
		runs, err := trader.GetBacktests(cfg)
		if err != nil {
			s.Log.Warn("service: skipping config", "path", cfgPath, "err", err)
			continue
		}
		for _, run := range runs {
			run := run // copy for closure capture safety
			summary, runErr := s.RunBacktest(ctx, &run)
			if runErr != nil {
				s.Log.Warn("service: backtest run failed",
					"name", run.Name, "err", runErr)
				continue
			}
			summaries = append(summaries, summary)
		}
	}
	return summaries, nil
}
