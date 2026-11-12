package trader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/rustyeddy/trader/num"
)

// Run backtests every scenario and writes one summary each to w. It is the
// entry point cmd/trader calls; see run for the output contract.
func Run(w io.Writer) error { return run(w) }

// scenarios are the reviewed datasets in emax-validation.org, in its order.
var scenarios = []string{
	"a-reversal-exit",
	"b1-stop-wick",
	"b2-stop-cross",
	"b3-stop-gap",
	"c-reversal",
	"d-short-stop",
}

// run backtests every scenario on fresh instances and writes one summary each
// to w. Diagnostics go to slog, on stderr, and carry wall-clock times; w gets
// only the summaries, so its output is identical from run to run.
func run(w io.Writer) error {
	for _, scenario := range scenarios {
		r, err := runScenario(scenario)
		if err != nil {
			return fmt.Errorf("scenario %s: %w", scenario, err)
		}
		if err := r.write(w); err != nil {
			return fmt.Errorf("scenario %s: write summary: %w", scenario, err)
		}
	}
	return nil
}

func runScenario(scenario string) (*result, error) {
	tr, err := getTrader()
	if err != nil {
		return nil, err
	}
	prices, err := loadBars(scenarioPath(scenario))
	if err != nil {
		return nil, err
	}

	slog.Info("backtest starting",
		"scenario", scenario,
		"strategy", tr.strategy.name(),
		"instrument", prices.instrument,
		"interval", prices.interval,
		"bars", len(prices.data))

	ctx := context.Background()
	if err := (&backtest{}).run(ctx, prices.bars(ctx), tr); err != nil {
		return nil, err
	}

	slog.Info("backtest complete", "scenario", scenario, "strategy", tr.strategy.name())
	return newResult(scenario, len(prices.data), tr)
}

// scenarioPath is where a scenario's bars are, relative to examples/design.
func scenarioPath(scenario string) string {
	return filepath.Join("testdata", "emax", scenario,
		"data", "synthetic", "canonical", "EURUSD", "2010", "01", "EURUSD-2010-01-D1.csv")
}

// getTrader builds the configuration the scenarios were worked out for.
func getTrader() (*trader, error) {
	strat := &emax{
		id:      "emax",
		short:   2,
		long:    4,
		stopPct: rateOnePercent,
	}
	if err := strat.init(); err != nil {
		return nil, err
	}
	br := newBroker("sim")
	return &trader{
		broker:   br,
		account:  br.addAccount("sim123", num.MustParseMoney("10000", usdCurrency)),
		strategy: strat,
	}, nil
}
