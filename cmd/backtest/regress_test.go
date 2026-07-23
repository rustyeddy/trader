package backtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader/backtest"
	backtestsvc "github.com/rustyeddy/trader/service/backtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── writeJSON / writeOrg ──────────────────────────────────────────────────────

func TestWriteJSON_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	s := backtest.BacktestReportSummary{Name: "test-run", Strategy: "ema", Trades: 3}

	require.NoError(t, backtestsvc.WriteBacktestSummaryJSON(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got backtest.BacktestReportSummary
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "test-run", got.Name)
	assert.Equal(t, 3, got.Trades)
}

func TestWriteJSON_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "report.json")
	s := backtest.BacktestReportSummary{Name: "nested"}

	require.NoError(t, backtestsvc.WriteBacktestSummaryJSON(path, s))
	assert.FileExists(t, path)
}

func TestWriteOrg_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.org")
	s := backtest.BacktestReportSummary{Name: "org-run", Strategy: "rsi"}

	require.NoError(t, backtestsvc.WriteBacktestSummaryOrg(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, data, "org file must not be empty")
}

// ── loadBaseline ──────────────────────────────────────────────────────────────

func TestLoadBaseline_ValidFile(t *testing.T) {
	dir := t.TempDir()
	s := backtest.BacktestReportSummary{Name: "myrun", Trades: 10, NetPL: 123.45}
	path := filepath.Join(dir, "myrun.json")
	require.NoError(t, backtestsvc.WriteBacktestSummaryJSON(path, s))

	got, err := loadBaseline(path)
	require.NoError(t, err)
	assert.Equal(t, s.Name, got.Name)
	assert.Equal(t, s.Trades, got.Trades)
	assert.Equal(t, s.NetPL, got.NetPL)
}

func TestLoadBaseline_MissingFile(t *testing.T) {
	_, err := loadBaseline("/nonexistent/path/run.json")
	assert.Error(t, err)
}

func TestLoadBaseline_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))

	_, err := loadBaseline(path)
	assert.Error(t, err)
}

// ── diffSummaries ─────────────────────────────────────────────────────────────

func TestDiffSummaries_Identical(t *testing.T) {
	s := backtest.BacktestReportSummary{
		Trades: 5, Wins: 3, Losses: 2,
		StartBalance: 10000, EndBalance: 10250, NetPL: 250,
		ReturnPct: 2.5, WinRate: 60, MaxDrawdown: -80,
		AvgWinner: 150, AvgLoser: -75, RR: 2.0,
		SpreadFiltered: 1, AvgSpreadPips: 1.2,
	}
	assert.Empty(t, diffSummaries(s, s))
}

func TestDiffSummaries_IntField(t *testing.T) {
	base := backtest.BacktestReportSummary{Trades: 10, Wins: 6, Losses: 4}
	got := backtest.BacktestReportSummary{Trades: 11, Wins: 6, Losses: 5}

	diffs := diffSummaries(base, got)
	require.Len(t, diffs, 2)
	assert.Contains(t, diffs[0], "trades")
	assert.Contains(t, diffs[1], "losses")
}

func TestDiffSummaries_FloatField(t *testing.T) {
	base := backtest.BacktestReportSummary{NetPL: 250.0, EndBalance: 10250.0}
	got := backtest.BacktestReportSummary{NetPL: 249.99, EndBalance: 10249.99}

	diffs := diffSummaries(base, got)
	require.Len(t, diffs, 2)
	assert.Contains(t, diffs[0], "end_balance")
	assert.Contains(t, diffs[1], "net_pl")
}

func TestDiffSummaries_AllFields(t *testing.T) {
	base := backtest.BacktestReportSummary{
		Trades: 5, Wins: 3, Losses: 2, SpreadFiltered: 1,
		StartBalance: 10000, EndBalance: 10250, NetPL: 250,
		ReturnPct: 2.5, WinRate: 60, MaxDrawdown: -80,
		AvgWinner: 150, AvgLoser: -75, RR: 2.0, AvgSpreadPips: 1.2,
	}
	got := backtest.BacktestReportSummary{} // all zero

	diffs := diffSummaries(base, got)
	// Every non-zero field in base should produce a diff.
	assert.Len(t, diffs, 14)
}

// ── updateBaselines ───────────────────────────────────────────────────────────

func TestUpdateBaselines_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	summaries := []backtest.BacktestReportSummary{
		{Name: "run-a", Trades: 10, NetPL: 200},
		{Name: "run-b", Trades: 5, NetPL: -50},
	}

	require.NoError(t, updateBaselines(dir, summaries))

	for _, s := range summaries {
		path := baselinePath(dir, s.Name)
		assert.FileExists(t, path)

		got, err := loadBaseline(path)
		require.NoError(t, err)
		assert.Equal(t, s.Trades, got.Trades)
		assert.Equal(t, s.NetPL, got.NetPL)
	}
}

func TestUpdateBaselines_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "nested")
	summaries := []backtest.BacktestReportSummary{{Name: "x"}}

	require.NoError(t, updateBaselines(dir, summaries))
	assert.FileExists(t, baselinePath(dir, "x"))
}

// ── compareBaselines ─────────────────────────────────────────────────────────

func TestCompareBaselines_AllPass(t *testing.T) {
	dir := t.TempDir()
	summaries := []backtest.BacktestReportSummary{
		{Name: "run-a", Trades: 10, NetPL: 200, WinRate: 60},
		{Name: "run-b", Trades: 5, NetPL: -50, WinRate: 40},
	}
	require.NoError(t, updateBaselines(dir, summaries))

	err := compareBaselines(dir, summaries)
	assert.NoError(t, err)
}

func TestCompareBaselines_Regression(t *testing.T) {
	dir := t.TempDir()
	baseline := backtest.BacktestReportSummary{Name: "run-a", Trades: 10, NetPL: 200}
	require.NoError(t, updateBaselines(dir, []backtest.BacktestReportSummary{baseline}))

	regressed := backtest.BacktestReportSummary{Name: "run-a", Trades: 9, NetPL: 150}
	err := compareBaselines(dir, []backtest.BacktestReportSummary{regressed})
	assert.ErrorContains(t, err, "regression")
}

func TestCompareBaselines_MissingBaseline(t *testing.T) {
	dir := t.TempDir() // empty — no baseline files
	summaries := []backtest.BacktestReportSummary{{Name: "run-a"}}

	err := compareBaselines(dir, summaries)
	assert.ErrorContains(t, err, "regression")
}
