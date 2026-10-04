package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/rustyeddy/trader/cmd/internal/backtestcfg"
	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"
	"github.com/rustyeddy/trader/internal/config"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/mcpserver"
	"github.com/rustyeddy/trader/internal/report"
	svcbacktest "github.com/rustyeddy/trader/internal/service/backtest"
)

// backtests implements mcpserver.Backtests over the same backtest
// composition `trader backtest run` uses (backtestcfg) and the shared
// run store `trader backtest show` reads (issue #437).
type backtests struct {
	factory *marketdatacfg.Factory
	store   *svcbacktest.RunStore
	// prepareData lets a run build canonical market data first. It is
	// the server's write policy: without --allow-writes, a run needs
	// data that is already current.
	prepareData bool
	logger      *slog.Logger
}

var _ mcpserver.Backtests = backtests{}

func (b backtests) Run(ctx context.Context, in mcpserver.BacktestInput, progress func(string)) (report.BacktestReport, error) {
	provider := in.Provider
	if provider == "" {
		provider = b.factory.DefaultProvider()
	}
	cfg, err := runConfig(in, provider)
	if err != nil {
		return report.BacktestReport{}, fmt.Errorf("%w: %w", mcpserver.ErrInvalidBacktest, err)
	}
	data, err := b.factory.Bundle(provider)
	if errors.Is(err, marketdatacfg.ErrUnknownProvider) {
		return report.BacktestReport{}, fmt.Errorf("%w: %w", mcpserver.ErrUnknownProvider, err)
	}
	if err != nil {
		return report.BacktestReport{}, err
	}
	rep, err := backtestcfg.Run(ctx, backtestcfg.Request{
		Config: cfg, Symbols: in.Symbols, WarmupBars: in.WarmupBars, Data: data,
		PrepareData: b.prepareData, Logger: b.logger, Progress: progress,
	})
	switch {
	case errors.Is(err, backtestcfg.ErrInvalidRun):
		return report.BacktestReport{}, fmt.Errorf("%w: %w", mcpserver.ErrInvalidBacktest, err)
	case errors.Is(err, backtestcfg.ErrDataNotReady):
		return report.BacktestReport{}, fmt.Errorf("%w: %w", mcpserver.ErrBacktestDataNotReady, err)
	case err != nil:
		return report.BacktestReport{}, err
	}
	if err := b.store.Save(rep); err != nil {
		return report.BacktestReport{}, err
	}
	return rep, nil
}

func (b backtests) Result(_ context.Context, runID string) (report.BacktestReport, error) {
	parsed, err := id.ParseRunID(runID)
	if err != nil {
		return report.BacktestReport{}, fmt.Errorf("%w: run id %q: %w", mcpserver.ErrInvalidBacktest, runID, err)
	}
	return b.store.Load(parsed)
}

// runConfig resolves in into a backtestcfg.RunConfig through the same
// config.Load the CLI uses, with in's fields as the overrides (named as
// the CLI's flags): defaults, decoding, and validation are therefore
// exactly `trader backtest run`'s. The server's environment is not
// consulted; a request says everything about its run.
func runConfig(in mcpserver.BacktestInput, provider string) (backtestcfg.RunConfig, error) {
	overrides := map[string]string{"provider": provider}
	set := func(key, value string) {
		if value != "" {
			overrides[key] = value
		}
	}
	set("interval", in.Interval)
	set("from", in.From)
	set("to", in.To)
	set("adverse-distance", in.AdverseDistance)
	set("starting-cash", in.StartingCapital)
	set("currency", in.Currency)
	set("risk-fraction", in.RiskFraction)
	set("initial-margin-ratio", in.InitialMarginRatio)
	set("strategy-name", in.Strategy.Name)
	set("allowed-side", in.Strategy.AllowedSide)
	set("quantity", in.Strategy.Quantity)
	set("buy-date", in.Strategy.BuyDate)
	set("sell-date", in.Strategy.SellDate)
	if in.Strategy.FastPeriod != nil {
		overrides["fast-period"] = strconv.Itoa(*in.Strategy.FastPeriod)
	}
	if in.Strategy.SlowPeriod != nil {
		overrides["slow-period"] = strconv.Itoa(*in.Strategy.SlowPeriod)
	}
	return config.Load[backtestcfg.RunConfig](config.Options{
		EnvPrefix: marketdatacfg.EnvPrefix,
		Environ:   []string{},
		Overrides: overrides,
	})
}
