package backtest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFixture serialises s as JSON into dir/<name>.json.
func writeFixture(t *testing.T, dir string, s trader.BacktestReportSummary) {
	t.Helper()
	b, err := json.MarshalIndent(s, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, s.Name+".json"), b, 0o644))
}

func TestResolveReportsDir(t *testing.T) {
	assert.Equal(t, "/custom/path", resolveReportsDir("/custom/path"))
	assert.Equal(t, "trimmed", resolveReportsDir("  trimmed  "))
	assert.Equal(t, filepath.Join(backtestBaseDir(), "reports"), resolveReportsDir(""))
	assert.Equal(t, filepath.Join(backtestBaseDir(), "reports"), resolveReportsDir("  "))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hello", truncate("hello", 5))
	assert.Equal(t, "hell…", truncate("hello!", 5))
	assert.Equal(t, "…", truncate("ab", 1))
	assert.Equal(t, "", truncate("ab", 0))
	assert.Equal(t, "", truncate("ab", -1))
}

func TestRunBacktestList_Empty(t *testing.T) {
	dir := t.TempDir()
	listReportsDir = dir
	defer func() { listReportsDir = "" }()

	var buf bytes.Buffer
	CMDBacktestList.SetOut(&buf)
	CMDBacktestList.SetErr(&buf)

	err := CMDBacktestList.RunE(CMDBacktestList, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No backtest results found")
}

func TestRunBacktestList_ShowsResults(t *testing.T) {
	dir := t.TempDir()
	listReportsDir = dir
	listInstrument = ""
	listStrategy = ""
	defer func() {
		listReportsDir = ""
		listInstrument = ""
		listStrategy = ""
	}()

	writeFixture(t, dir, trader.BacktestReportSummary{
		Name:       "eurusd-bbfade-abc12345",
		Strategy:   "BBFade",
		Instrument: "EUR_USD",
		Timeframe:  "H1",
		Trades:     42,
		WinRate:    55.0,
		RR:         1.5,
		ReturnPct:  4.2,
		NetPL:      420.0,
	})
	writeFixture(t, dir, trader.BacktestReportSummary{
		Name:       "gbpusd-macd-def67890",
		Strategy:   "MACD",
		Instrument: "GBP_USD",
		Timeframe:  "H4",
		Trades:     10,
		WinRate:    40.0,
		RR:         0,
		ReturnPct:  -1.5,
		NetPL:      -150.0,
	})

	var buf bytes.Buffer
	CMDBacktestList.SetOut(&buf)
	CMDBacktestList.SetErr(&buf)

	err := CMDBacktestList.RunE(CMDBacktestList, nil)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "EUR_USD")
	assert.Contains(t, out, "GBP_USD")
	assert.Contains(t, out, "eurusd-bbfade-abc12345")
	assert.Contains(t, out, "2 result(s)")
}

func TestRunBacktestList_FilterInstrument(t *testing.T) {
	dir := t.TempDir()
	listReportsDir = dir
	listInstrument = "EUR"
	listStrategy = ""
	defer func() {
		listReportsDir = ""
		listInstrument = ""
		listStrategy = ""
	}()

	writeFixture(t, dir, trader.BacktestReportSummary{
		Name: "eurusd-run", Instrument: "EUR_USD", Strategy: "BBFade",
	})
	writeFixture(t, dir, trader.BacktestReportSummary{
		Name: "gbpusd-run", Instrument: "GBP_USD", Strategy: "BBFade",
	})

	var buf bytes.Buffer
	CMDBacktestList.SetOut(&buf)
	CMDBacktestList.SetErr(&buf)

	err := CMDBacktestList.RunE(CMDBacktestList, nil)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "EUR_USD")
	assert.NotContains(t, out, "GBP_USD")
	assert.Contains(t, out, "1 result(s)")
}

func TestRunBacktestGet_NotFound(t *testing.T) {
	dir := t.TempDir()
	getReportsDir = dir
	defer func() { getReportsDir = "" }()

	err := CMDBacktestGet.RunE(CMDBacktestGet, []string{"nonexistent"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "nonexistent"))
}

func TestRunBacktestGet_Found(t *testing.T) {
	dir := t.TempDir()
	getReportsDir = dir
	defer func() { getReportsDir = "" }()

	writeFixture(t, dir, trader.BacktestReportSummary{
		Name:         "eurusd-bbfade-abc12345",
		Strategy:     "BBFade",
		Instrument:   "EUR_USD",
		Timeframe:    "H1",
		Trades:       42,
		StartBalance: 10000,
		EndBalance:   10420,
		NetPL:        420.0,
		ReturnPct:    4.2,
		WinRate:      55.0,
	})

	var buf bytes.Buffer
	CMDBacktestGet.SetOut(&buf)
	CMDBacktestGet.SetErr(&buf)

	err := CMDBacktestGet.RunE(CMDBacktestGet, []string{"eurusd-bbfade-abc12345"})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "BBFade")
	assert.Contains(t, out, "EUR_USD")
}
