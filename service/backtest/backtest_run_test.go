package backtestsvc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Register "noop" strategy for compile-phase tests.
	"github.com/rustyeddy/trader/backtest"
	_ "github.com/rustyeddy/trader/strategies/noop"
	"github.com/rustyeddy/trader/strategy"
)

// stubExecutor is a BacktestExecutor that either succeeds or returns a canned
// error without touching the run's internals.
type stubExecutor struct{ err error }

func (s stubExecutor) Execute(_ context.Context, _ *backtest.Backtest) error { return s.err }

// minCompiledBacktest returns a CompiledBacktest whose Request is populated
// enough to satisfy RunBacktest's nil-request guard.
func minCompiledBacktest(t *testing.T) backtest.CompiledBacktest {
	t.Helper()
	cfg := &backtest.Config{
		Defaults: backtest.RunDefaults{StartingBalance: 1000},
		Runs: []backtest.RunConfig{{
			Name: "svc-unit-test",
			Data: backtest.DataConfig{
				Instrument: "EURUSD",
				Timeframe:  "H1",
				From:       "2026-01-01",
				To:         "2026-01-10",
			},
			Strategy: strategy.StrategyConfig{Kind: "noop"},
		}},
	}
	runs, err := backtest.CompileBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	return runs[0]
}

// minYAMLConfig writes a valid single-run YAML config using the noop strategy
// to dir and returns its path.
func minYAMLConfig(t *testing.T, dir, name string) string {
	t.Helper()
	content := `defaults:
  starting-balance: 1000
runs:
  - name: ` + name + `
    data:
      instrument: EURUSD
      timeframe: H1
      from: "2026-01-01"
      to: "2026-01-10"
    strategy:
      kind: noop
`
	path := filepath.Join(dir, name+".yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func newBacktestService() *Service {
	return &Service{Log: slog.Default()}
}

// ---------------------------------------------------------------------------
// RunBacktest
// ---------------------------------------------------------------------------

func TestRunBacktest_Success(t *testing.T) {
	svc := newBacktestService()
	svc.Executor = stubExecutor{err: nil}

	compiled := minCompiledBacktest(t)
	_, err := svc.RunBacktest(context.Background(), compiled)
	require.NoError(t, err)
}

func TestRunBacktest_ExecutorErrorIsWrapped(t *testing.T) {
	svc := newBacktestService()
	svc.Executor = stubExecutor{err: errors.New("sim failure")}

	compiled := minCompiledBacktest(t)
	_, err := svc.RunBacktest(context.Background(), compiled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sim failure")
	assert.Contains(t, err.Error(), "svc-unit-test")
}

func TestRunBacktest_FallbackExecutorUsedWhenNilBacktests(t *testing.T) {
	// Service.Backtests is nil → backtestExecutor() constructs a real
	// TraderBacktestExecutor. With noop strategy and no candle data available
	// the run completes with zero trades — the important invariant is no panic.
	svc := newBacktestService()
	compiled := minCompiledBacktest(t)
	summary, err := svc.RunBacktest(context.Background(), compiled)
	require.NoError(t, err)
	assert.Zero(t, summary.Trades)
}

// ---------------------------------------------------------------------------
// RunBacktestConfigs
// ---------------------------------------------------------------------------

func TestRunBacktestConfigs_Success(t *testing.T) {
	dir := t.TempDir()
	minYAMLConfig(t, dir, "run-a")
	minYAMLConfig(t, dir, "run-b")

	svc := newBacktestService()
	svc.Executor = stubExecutor{}

	summaries, err := svc.RunBacktestConfigs(context.Background(), []string{
		filepath.Join(dir, "run-a.yml"),
		filepath.Join(dir, "run-b.yml"),
	})
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
}

func TestRunBacktestConfigs_BadConfigPathReturnsError(t *testing.T) {
	svc := newBacktestService()
	svc.Executor = stubExecutor{}

	_, err := svc.RunBacktestConfigs(context.Background(), []string{"/nonexistent/config.yml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}

func TestRunBacktestConfigs_BadRunIsSkipped(t *testing.T) {
	// Executor always fails → runs are skipped via Warn, outer error is nil.
	dir := t.TempDir()
	minYAMLConfig(t, dir, "run-err")

	svc := newBacktestService()
	svc.Executor = stubExecutor{err: errors.New("always fails")}

	summaries, err := svc.RunBacktestConfigs(context.Background(), []string{
		filepath.Join(dir, "run-err.yml"),
	})
	require.NoError(t, err)
	assert.Empty(t, summaries)
}

// ---------------------------------------------------------------------------
// RunBacktestPathSpecs
// ---------------------------------------------------------------------------

func TestRunBacktestPathSpecs_DirectorySpec(t *testing.T) {
	dir := t.TempDir()
	minYAMLConfig(t, dir, "run-x")

	svc := newBacktestService()
	svc.Executor = stubExecutor{}

	summaries, err := svc.RunBacktestPathSpecs(context.Background(), []string{dir})
	require.NoError(t, err)
	assert.Len(t, summaries, 1)
}

func TestRunBacktestPathSpecs_EmptySpecsReturnsError(t *testing.T) {
	svc := newBacktestService()
	_, err := svc.RunBacktestPathSpecs(context.Background(), nil)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// RunBacktestConfigsAndWriteReports
// ---------------------------------------------------------------------------

func TestRunBacktestConfigsAndWriteReports_WritesFiles(t *testing.T) {
	cfgDir := t.TempDir()
	outDir := t.TempDir()
	minYAMLConfig(t, cfgDir, "rpt-run")

	svc := newBacktestService()
	svc.Executor = stubExecutor{}

	summaries, err := svc.RunBacktestConfigsAndWriteReports(
		context.Background(),
		[]string{filepath.Join(cfgDir, "rpt-run.yml")},
		outDir,
	)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	// At least one JSON and one .org file should exist in outDir.
	jsonFiles, _ := filepath.Glob(filepath.Join(outDir, "*.json"))
	assert.NotEmpty(t, jsonFiles)
	orgFiles, _ := filepath.Glob(filepath.Join(outDir, "*.org"))
	assert.NotEmpty(t, orgFiles)
}

func TestRunBacktestConfigsAndWriteReports_NoResultsReturnsError(t *testing.T) {
	// Executor always fails → no summaries → should return an error.
	cfgDir := t.TempDir()
	minYAMLConfig(t, cfgDir, "fail-run")

	svc := newBacktestService()
	svc.Executor = stubExecutor{err: errors.New("always fails")}

	_, err := svc.RunBacktestConfigsAndWriteReports(
		context.Background(),
		[]string{filepath.Join(cfgDir, "fail-run.yml")},
		t.TempDir(),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backtest results")
}

// ---------------------------------------------------------------------------
// RunBacktestPathSpecsAndWriteReports
// ---------------------------------------------------------------------------

func TestRunBacktestPathSpecsAndWriteReports_WritesFiles(t *testing.T) {
	cfgDir := t.TempDir()
	outDir := t.TempDir()
	minYAMLConfig(t, cfgDir, "ps-run")

	svc := newBacktestService()
	svc.Executor = stubExecutor{}

	summaries, err := svc.RunBacktestPathSpecsAndWriteReports(
		context.Background(),
		[]string{cfgDir},
		outDir,
	)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	jsonFiles, _ := filepath.Glob(filepath.Join(outDir, "*.json"))
	assert.NotEmpty(t, jsonFiles)
}

// ---------------------------------------------------------------------------
// ReadBacktestSummaryFile
// ---------------------------------------------------------------------------

func TestReadBacktestSummaryFile_ParsesJSONAndSetsName(t *testing.T) {
	dir := t.TempDir()
	summary := backtest.BacktestReportSummary{
		Name:       "original-name",
		Strategy:   "noop",
		Instrument: "EURUSD",
		Trades:     5,
	}
	b, err := json.MarshalIndent(summary, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(dir, "canonical-stem.json")
	require.NoError(t, os.WriteFile(path, append(b, '\n'), 0o644))

	got, err := ReadBacktestSummaryFile(path)
	require.NoError(t, err)
	// Name is overwritten with the filename stem (canonical behaviour).
	assert.Equal(t, "canonical-stem", got.Name)
	assert.Equal(t, summary.Strategy, got.Strategy)
	assert.Equal(t, summary.Trades, got.Trades)
}

func TestReadBacktestSummaryFile_MissingFileReturnsError(t *testing.T) {
	_, err := ReadBacktestSummaryFile(filepath.Join(t.TempDir(), "gone.json"))
	require.Error(t, err)
}

func TestReadBacktestSummaryFile_MalformedJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))
	_, err := ReadBacktestSummaryFile(path)
	require.Error(t, err)
}
