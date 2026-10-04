package backtestcfg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"
	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/num"
)

func TestLoadOutputDir(t *testing.T) {
	dir, err := LoadOutputDir([]string{}, "", "")
	require.NoError(t, err)
	assert.Equal(t, DefaultOutputDir, dir)

	dir, err = LoadOutputDir([]string{"TRADER_BACKTEST_OUTPUT_DIR=/env/runs"}, "", "")
	require.NoError(t, err)
	assert.Equal(t, "/env/runs", dir)

	dir, err = LoadOutputDir([]string{"TRADER_BACKTEST_OUTPUT_DIR=/env/runs"}, "", "/flag/runs")
	require.NoError(t, err)
	assert.Equal(t, "/flag/runs", dir, "an explicit flag wins")

	// A full run config file (backtest and strategy sections) is read
	// for its backtest.output_dir alone.
	cfgFile := filepath.Join(t.TempDir(), "run.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte("backtest:\n  symbol: EURUSD\n  from: 2024-01-01\n  output_dir: /file/runs\nstrategy:\n  name: ema-cross\n"), 0o644))
	dir, err = LoadOutputDir([]string{}, cfgFile, "")
	require.NoError(t, err)
	assert.Equal(t, "/file/runs", dir)
	dir, err = LoadOutputDir([]string{"TRADER_BACKTEST_OUTPUT_DIR=/env/runs"}, cfgFile, "")
	require.NoError(t, err)
	assert.Equal(t, "/env/runs", dir, "the environment overrides the file")

	_, err = LoadOutputDir([]string{}, filepath.Join(t.TempDir(), "missing.yaml"), "")
	assert.Error(t, err)
}

// runRequest is a demo-strategy run over the CLI's EURUSD fixture,
// against a store at storeRoot.
func runRequest(t *testing.T, storeRoot string, prepare bool) Request {
	t.Helper()
	data, err := marketdatacfg.New(marketdatacfg.Config{StoreRoot: storeRoot, RawRoot: cliTestdataRaw, Provider: "oanda"}, logging.Discard())
	require.NoError(t, err)
	return Request{
		Config: RunConfig{
			Backtest: BacktestSection{
				Interval: "H1", From: "2024-01-08T00:00:00Z", To: "2024-01-08T04:00:00Z",
				Currency: "USD", StartingCapital: "10000", RiskFraction: num.MustParseRate("0.01"),
				AdverseDistance: num.MustParsePrice("0.01"), InitialMarginRatio: num.MustParseRate("1"), Provider: "oanda",
			},
			Strategy: StrategySection{Name: demoStrategyName},
		},
		Symbols:     []string{"EURUSD"},
		Data:        data,
		PrepareData: prepare,
	}
}

func TestRun_PrepareDataModes(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()

	_, err := Run(ctx, runRequest(t, store, false))
	require.ErrorIs(t, err, ErrDataNotReady, "without PrepareData, missing canonical data is not built")
	assert.NotErrorIs(t, err, ErrInvalidRun, "data readiness is not an input error")
	assert.ErrorContains(t, err, "EURUSD H1 2024-01 is missing")
	entries, err := os.ReadDir(store)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing was built")

	rep, err := Run(ctx, runRequest(t, store, true))
	require.NoError(t, err)
	assert.NotEmpty(t, rep.Run.RunID)
	assert.Equal(t, demoStrategyName, rep.Run.StrategyName)

	again, err := Run(ctx, runRequest(t, store, false))
	require.NoError(t, err, "once the data is current, a run needs no build")
	assert.Equal(t, rep.Run.ConfigDigest, again.Run.ConfigDigest, "same inputs over the same store: same config digest")
}

func TestRun_RequiresMarketData(t *testing.T) {
	req := runRequest(t, t.TempDir(), true)
	req.Data = marketdatacfg.Bundle{}
	_, err := Run(context.Background(), req)
	assert.ErrorContains(t, err, "market data is not configured")
}

func TestRun_ValidatesItsConfig(t *testing.T) {
	req := runRequest(t, t.TempDir(), true)
	req.Config.Strategy.Name = "typo"
	_, err := Run(context.Background(), req)
	require.Error(t, err, "an unregistered strategy must fail, not run the default")
	assert.ErrorIs(t, err, ErrInvalidRun, "an input error is marked")
	assert.ErrorContains(t, err, `strategy.name "typo" is not registered`)
	assert.NotContains(t, err.Error(), ErrInvalidRun.Error(), "the marker leaves the message unchanged")

	req = runRequest(t, t.TempDir(), true)
	req.Config.Backtest.InitialMarginRatio = num.MustParseRate("0")
	_, err = Run(context.Background(), req)
	assert.ErrorContains(t, err, "initial_margin_ratio must be positive")
}

func TestRun_SingleInstrumentStrategiesRejectSeveralSymbols(t *testing.T) {
	store := t.TempDir()
	for name, mutate := range map[string]func(*Request){
		"ema-cross": func(r *Request) {
			r.Config.Strategy = StrategySection{Name: "ema-cross", FastPeriod: 3, SlowPeriod: 5}
		},
		"quantity mode": func(r *Request) {
			r.Config.Strategy = StrategySection{Name: demoStrategyName, Quantity: "1"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := runRequest(t, store, true)
			req.Symbols = []string{"EURUSD", "GBPUSD"}
			mutate(&req)
			_, err := Run(context.Background(), req)
			assert.ErrorContains(t, err, "exactly one instrument")
			assert.ErrorIs(t, err, ErrInvalidRun)
			entries, err := os.ReadDir(store)
			require.NoError(t, err)
			assert.Empty(t, entries, "rejected before any data is prepared")
		})
	}
}

func TestRun_ReportsProgressStages(t *testing.T) {
	var stages []string
	req := runRequest(t, t.TempDir(), true)
	req.Progress = func(stage string) { stages = append(stages, stage) }
	_, err := Run(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, []string{StagePreparingData, StageRunning}, stages)
}

func TestRun_InvalidSymbolsAreInputErrors(t *testing.T) {
	for name, symbols := range map[string][]string{
		"none":      nil,
		"duplicate": {"EURUSD", "eurusd"},
		"bad fx":    {"EUR"},
	} {
		t.Run(name, func(t *testing.T) {
			req := runRequest(t, t.TempDir(), true)
			req.Symbols = symbols
			_, err := Run(context.Background(), req)
			assert.ErrorIs(t, err, ErrInvalidRun)
		})
	}
}

func TestInputErrorUnwraps(t *testing.T) {
	base := errors.New("base")
	err := invalid(base)
	assert.ErrorIs(t, err, base)
	assert.ErrorIs(t, err, ErrInvalidRun)
	assert.EqualError(t, err, "base")
	assert.NoError(t, invalid(nil))
}
