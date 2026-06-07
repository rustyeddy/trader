package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteBacktestReports_WritesHashNamedReportsAndIndex(t *testing.T) {
	dir := t.TempDir()
	summary := trader.BacktestReportSummary{
		Name:       "eurusd-h1-emacross",
		ConfigHash: "abc12345",
		Strategy:   "ema-cross",
		Instrument: "EURUSD",
		Timeframe:  "H1",
		Trades:     7,
	}

	require.NoError(t, WriteBacktestReports(dir, []trader.BacktestReportSummary{summary}))

	jsonPath := filepath.Join(dir, "eurusd-h1-emacross-abc12345.json")
	orgPath := filepath.Join(dir, "eurusd-h1-emacross-abc12345.org")
	indexPath := filepath.Join(dir, "index.org")

	assert.FileExists(t, jsonPath)
	assert.FileExists(t, orgPath)
	assert.FileExists(t, indexPath)

	data, err := os.ReadFile(jsonPath)
	require.NoError(t, err)

	var got trader.BacktestReportSummary
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, summary.Name, got.Name)
	assert.Equal(t, summary.ConfigHash, got.ConfigHash)
	assert.Equal(t, summary.Trades, got.Trades)
}

func TestWriteBacktestReports_FallsBackToNameWithoutHash(t *testing.T) {
	dir := t.TempDir()
	summary := trader.BacktestReportSummary{Name: "run-without-hash", Strategy: "ema-cross"}

	require.NoError(t, WriteBacktestReports(dir, []trader.BacktestReportSummary{summary}))

	assert.FileExists(t, filepath.Join(dir, "run-without-hash.json"))
	assert.FileExists(t, filepath.Join(dir, "run-without-hash.org"))
}

func TestWriteBacktestSummaryJSON_CreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "reports", "run.json")

	require.NoError(t, WriteBacktestSummaryJSON(path, trader.BacktestReportSummary{Name: "run"}))
	assert.FileExists(t, path)
}

func TestWriteBacktestSummaryOrg_CreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "reports", "run.org")

	require.NoError(t, WriteBacktestSummaryOrg(path, trader.BacktestReportSummary{Name: "run", Strategy: "ema-cross"}))
	assert.FileExists(t, path)
}

func TestRebuildBacktestIndex_WritesComparisonTable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteBacktestSummaryJSON(filepath.Join(dir, "run-a.json"), trader.BacktestReportSummary{
		Name:       "run-a",
		Strategy:   "ema-cross",
		Instrument: "EURUSD",
		Timeframe:  "H1",
	}))
	require.NoError(t, WriteBacktestSummaryJSON(filepath.Join(dir, "run-b.json"), trader.BacktestReportSummary{
		Name:       "run-b",
		Strategy:   "donchian",
		Instrument: "USDJPY",
		Timeframe:  "D1",
	}))

	require.NoError(t, RebuildBacktestIndex(dir))

	indexPath := filepath.Join(dir, "index.org")
	assert.FileExists(t, indexPath)

	data, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "run-a")
	assert.Contains(t, string(data), "run-b")
}

func TestBacktestReportStem(t *testing.T) {
	assert.Equal(t, "run-a-deadbeef", backtestReportStem(trader.BacktestReportSummary{
		Name:       "run-a",
		ConfigHash: "deadbeef",
	}))
	assert.Equal(t, "run-b", backtestReportStem(trader.BacktestReportSummary{Name: "run-b"}))
}

func TestListBacktestSummaries_SortsNewestFilenameFirst(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteBacktestSummaryJSON(filepath.Join(dir, "run-a.json"), trader.BacktestReportSummary{Name: "run-a"}))
	require.NoError(t, WriteBacktestSummaryJSON(filepath.Join(dir, "run-b.json"), trader.BacktestReportSummary{Name: "run-b"}))

	summaries, err := ListBacktestSummaries(dir)
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	assert.Equal(t, "run-b", summaries[0].Name)
	assert.Equal(t, "run-a", summaries[1].Name)
}

func TestReadBacktestSummaryByName_UsesFilenameAsCanonicalName(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteBacktestSummaryJSON(filepath.Join(dir, "run-a-deadbeef.json"), trader.BacktestReportSummary{Name: "run-a"}))

	summary, err := ReadBacktestSummaryByName(dir, "run-a-deadbeef")
	require.NoError(t, err)
	assert.Equal(t, "run-a-deadbeef", summary.Name)
}

func TestListBacktestOrgReports_ReturnsSortedBasenames(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.org"), []byte("* b\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.org"), []byte("* a\n"), 0o644))

	names, err := ListBacktestOrgReports(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"a.org", "b.org"}, names)
}

func TestReadBacktestOrgReport_AppendsSuffix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run-a.org"), []byte("* a\n"), 0o644))

	data, filename, err := ReadBacktestOrgReport(dir, "run-a")
	require.NoError(t, err)
	assert.Equal(t, "run-a.org", filename)
	assert.Equal(t, "* a\n", string(data))
}
