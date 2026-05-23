package backtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timestampRE matches the _YYYYMMDD-HHMMSS suffix added to run report filenames.
var timestampRE = regexp.MustCompile(`_\d{8}-\d{6}$`)

// ── writeJSON / writeOrg ──────────────────────────────────────────────────────

func TestWriteJSON_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	s := trader.BacktestReportSummary{Name: "test-run", Strategy: "ema", Trades: 3}

	require.NoError(t, writeJSON(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got trader.BacktestReportSummary
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "test-run", got.Name)
	assert.Equal(t, 3, got.Trades)
}

func TestWriteJSON_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "report.json")
	s := trader.BacktestReportSummary{Name: "nested"}

	require.NoError(t, writeJSON(path, s))
	assert.FileExists(t, path)
}

func TestWriteOrg_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.org")
	s := trader.BacktestReportSummary{Name: "org-run", Strategy: "rsi"}

	require.NoError(t, writeOrg(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, data, "org file must not be empty")
}

// ── timestamp naming convention ───────────────────────────────────────────────

func TestReportFilenameHasTimestampSuffix(t *testing.T) {
	stem := "eurusd-h1-ema-cross" + "_" + "20260520-221547"
	base := filepath.Base(stem + ".json")
	name := base[:len(base)-len(".json")]

	idx := len("eurusd-h1-ema-cross")
	suffix := name[idx:]
	assert.Regexp(t, `^_\d{8}-\d{6}$`, suffix)
}

func TestTimestampRE(t *testing.T) {
	cases := []struct {
		stem  string
		match bool
	}{
		{"run-a_20260520-221547", true},
		{"eurusd-h1-ema_20000101-000000", true},
		{"run-a", false},
		{"run-a_2026052-221547", false},
		{"run-a_20260520-22154", false},
	}
	for _, tc := range cases {
		got := timestampRE.MatchString(tc.stem)
		assert.Equal(t, tc.match, got, "stem=%q", tc.stem)
	}
}

// ── loadBaseline ──────────────────────────────────────────────────────────────

func TestLoadBaseline_ValidFile(t *testing.T) {
	dir := t.TempDir()
	s := trader.BacktestReportSummary{Name: "myrun", Trades: 10, NetPL: 123.45}
	path := filepath.Join(dir, "myrun.json")
	require.NoError(t, writeJSON(path, s))

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
	s := trader.BacktestReportSummary{
		Trades: 5, Wins: 3, Losses: 2,
		StartBalance: 10000, EndBalance: 10250, NetPL: 250,
		ReturnPct: 2.5, WinRate: 60, MaxDrawdown: -80,
		AvgWinner: 150, AvgLoser: -75, RR: 2.0,
		SpreadFiltered: 1, AvgSpreadPips: 1.2,
	}
	assert.Empty(t, diffSummaries(s, s))
}

func TestDiffSummaries_IntField(t *testing.T) {
	base := trader.BacktestReportSummary{Trades: 10, Wins: 6, Losses: 4}
	got := trader.BacktestReportSummary{Trades: 11, Wins: 6, Losses: 5}

	diffs := diffSummaries(base, got)
	require.Len(t, diffs, 2)
	assert.Contains(t, diffs[0], "trades")
	assert.Contains(t, diffs[1], "losses")
}

func TestDiffSummaries_FloatField(t *testing.T) {
	base := trader.BacktestReportSummary{NetPL: 250.0, EndBalance: 10250.0}
	got := trader.BacktestReportSummary{NetPL: 249.99, EndBalance: 10249.99}

	diffs := diffSummaries(base, got)
	require.Len(t, diffs, 2)
	assert.Contains(t, diffs[0], "end_balance")
	assert.Contains(t, diffs[1], "net_pl")
}

func TestDiffSummaries_AllFields(t *testing.T) {
	base := trader.BacktestReportSummary{
		Trades: 5, Wins: 3, Losses: 2, SpreadFiltered: 1,
		StartBalance: 10000, EndBalance: 10250, NetPL: 250,
		ReturnPct: 2.5, WinRate: 60, MaxDrawdown: -80,
		AvgWinner: 150, AvgLoser: -75, RR: 2.0, AvgSpreadPips: 1.2,
	}
	got := trader.BacktestReportSummary{} // all zero

	diffs := diffSummaries(base, got)
	// Every non-zero field in base should produce a diff.
	assert.Len(t, diffs, 14)
}

// ── updateBaselines ───────────────────────────────────────────────────────────

func TestUpdateBaselines_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	summaries := []trader.BacktestReportSummary{
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
	summaries := []trader.BacktestReportSummary{{Name: "x"}}

	require.NoError(t, updateBaselines(dir, summaries))
	assert.FileExists(t, baselinePath(dir, "x"))
}

// ── compareBaselines ─────────────────────────────────────────────────────────

func TestCompareBaselines_AllPass(t *testing.T) {
	dir := t.TempDir()
	summaries := []trader.BacktestReportSummary{
		{Name: "run-a", Trades: 10, NetPL: 200, WinRate: 60},
		{Name: "run-b", Trades: 5, NetPL: -50, WinRate: 40},
	}
	require.NoError(t, updateBaselines(dir, summaries))

	err := compareBaselines(dir, summaries)
	assert.NoError(t, err)
}

func TestCompareBaselines_Regression(t *testing.T) {
	dir := t.TempDir()
	baseline := trader.BacktestReportSummary{Name: "run-a", Trades: 10, NetPL: 200}
	require.NoError(t, updateBaselines(dir, []trader.BacktestReportSummary{baseline}))

	regressed := trader.BacktestReportSummary{Name: "run-a", Trades: 9, NetPL: 150}
	err := compareBaselines(dir, []trader.BacktestReportSummary{regressed})
	assert.ErrorContains(t, err, "regression")
}

func TestCompareBaselines_MissingBaseline(t *testing.T) {
	dir := t.TempDir() // empty — no baseline files
	summaries := []trader.BacktestReportSummary{{Name: "run-a"}}

	err := compareBaselines(dir, summaries)
	assert.ErrorContains(t, err, "regression")
}
