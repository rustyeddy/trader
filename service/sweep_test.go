//go:build sweep

package service

// Strategy sweep — runs every real strategy over every instrument and
// timeframe combination that has local candle data.
//
// Usage:
//
//	go test -tags sweep -timeout 15m -v ./service/... -run TestStrategySweep
//	make sweep
//
// Pass criteria: no panic, no crash. A run that returns zero trades or
// negative P&L is not a failure — the sweep is a correctness gate,
// not a profitability filter.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rustyeddy/trader"

	// Register all real strategies via init().
	_ "github.com/rustyeddy/trader/strategies/bollingerfade"
	_ "github.com/rustyeddy/trader/strategies/donchian"
	_ "github.com/rustyeddy/trader/strategies/donchianv2"
	_ "github.com/rustyeddy/trader/strategies/donchianv3"
	_ "github.com/rustyeddy/trader/strategies/donchianv4"
	_ "github.com/rustyeddy/trader/strategies/donchianv5"
	_ "github.com/rustyeddy/trader/strategies/donchianv6"
	_ "github.com/rustyeddy/trader/strategies/emacross"
	_ "github.com/rustyeddy/trader/strategies/emacrossadx"
)

// sweepStrategy pairs a strategy kind with any params it requires to run.
// Aliases (donchian-breakout-*, bollinger-fade) are omitted to avoid
// re-testing the same constructor twice.
type sweepStrategy struct {
	kind   string
	params map[string]any
}

var sweepStrategies = []sweepStrategy{
	{kind: "donchian"},
	{kind: "donchian-v2"},
	{kind: "donchian-v3"},
	{kind: "donchian-v4"},
	{kind: "donchian-v5"},
	{kind: "donchian-v6"},
	{kind: "ema-cross", params: map[string]any{
		"fast": 9,
		"slow": 21,
	}},
	{kind: "ema-cross-adx", params: map[string]any{
		"fast":          9,
		"slow":          21,
		"adx_period":    14,
		"adx_threshold": 20.0,
	}},
	{kind: "bb-fade"},
}

// sweepMatrix defines what to run each strategy against.
// H1 uses a single year (fast); D1 uses three years (enough daily warmup).
var sweepMatrix = []struct {
	timeframe string
	from      string
	to        string
}{
	{"H1", "2024-01-01", "2024-12-31"},
	{"D",  "2022-01-01", "2024-12-31"},
}

// sweepInstruments are all instruments that have local candle data.
var sweepInstruments = []string{
	"EURUSD", "GBPUSD", "USDJPY", "USDCHF", "USDCAD",
	"AUDUSD", "EURGBP", "EURJPY", "GBPJPY", "AUDJPY", "NZDUSD",
}

// sweepDefaults are the account settings applied to every run.
var sweepDefaults = trader.RunDefaults{
	StartingBalance: 10_000,
	AccountCCY:      "USD",
	RiskPct:         0.5,
	StopPips:        20,
	TakePips:        40,
	Source:          "oanda",
}

func TestStrategySweep(t *testing.T) {
	// Quiet logger — sweep output is the summary table, not per-bar logs.
	trader.Setup(trader.LogConfig{Level: "error", Stdout: true}) //nolint:errcheck
	svc := &Service{Log: slog.Default()}

	var (
		mu       sync.Mutex
		passed   int64
		failed   int64
		skipped  int64
		failures []string
	)

	for _, strategy := range sweepStrategies {
		for _, tf := range sweepMatrix {
			for _, inst := range sweepInstruments {
				// Capture loop vars for parallel subtests.
				strategy, tf, inst := strategy, tf, inst
				name := fmt.Sprintf("%s/%s/%s", strategy.kind, tf.timeframe, inst)

				t.Run(name, func(t *testing.T) {
					t.Parallel()

					cfg := &trader.Config{
						Defaults: sweepDefaults,
						Runs: []trader.RunConfig{{
							Name: name,
							Data: trader.DataConfig{
								Source:     "oanda",
								Instrument: inst,
								Timeframe:  tf.timeframe,
								From:       tf.from,
								To:         tf.to,
							},
							Strategy: trader.StrategyConfig{Kind: strategy.kind, Params: strategy.params},
							// Chandelier exit ensures strategies that delegate
							// stop calculation (donchian-v3+) produce valid stops.
							Exit: trader.ExitConfig{
								Kind: "chandelier",
								Params: map[string]any{
									"atr_period":  14,
									"multiplier":  3.0,
								},
							},
						}},
					}

					runs, err := trader.GetBacktests(cfg)
					if err != nil {
						// Strategy or config not compatible — skip, not fail.
						mu.Lock()
						skipped++
						mu.Unlock()
						t.Skipf("skipped: %v", err)
						return
					}

					run := runs[0]
					_, runErr := svc.RunBacktest(context.Background(), &run)
					if runErr != nil {
						atomic.AddInt64(&failed, 1)
						mu.Lock()
						failures = append(failures, fmt.Sprintf("  FAIL  %s: %v", name, runErr))
						mu.Unlock()
						t.Errorf("run failed: %v", runErr)
						return
					}
					atomic.AddInt64(&passed, 1)
				})
			}
		}
	}

	// Print summary after all subtests complete.
	t.Cleanup(func() {
		total := passed + failed + skipped
		fmt.Fprintf(os.Stdout, "\n=== Strategy Sweep Summary ===\n")
		fmt.Fprintf(os.Stdout, "  Total : %d\n", total)
		fmt.Fprintf(os.Stdout, "  Passed: %d\n", passed)
		fmt.Fprintf(os.Stdout, "  Failed: %d\n", failed)
		fmt.Fprintf(os.Stdout, "  Skip  : %d  (strategy/instrument mismatch)\n", skipped)
		if len(failures) > 0 {
			fmt.Fprintf(os.Stdout, "\nFailures:\n")
			for _, f := range failures {
				fmt.Fprintln(os.Stdout, f)
			}
		}
		fmt.Fprintln(os.Stdout)
	})
}
