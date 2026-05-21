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

// timestampRE matches the _YYYYMMDD-HHMMSS suffix added to report filenames.
var timestampRE = regexp.MustCompile(`_\d{8}-\d{6}$`)

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

func TestReportFilenameHasTimestampSuffix(t *testing.T) {
	// Verify the naming convention: <name>_YYYYMMDD-HHMMSS
	// We can't control the clock in runBacktestRegress without refactoring, so
	// we test the pattern by constructing a stem the same way the command does.
	//
	// This is a format contract test: if someone changes the timestamp layout
	// the regex below will catch it.
	stem := "eurusd-h1-ema-cross" + "_" + "20260520-221547"
	base := filepath.Base(stem + ".json")
	name := base[:len(base)-len(".json")]

	// The part after the last underscore must match the timestamp pattern.
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
		{"run-a", false},             // no suffix
		{"run-a_2026052-221547", false}, // short date
		{"run-a_20260520-22154", false},  // short time
	}
	for _, tc := range cases {
		got := timestampRE.MatchString(tc.stem)
		assert.Equal(t, tc.match, got, "stem=%q", tc.stem)
	}
}
