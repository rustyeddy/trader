package backtest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── backtest org ──────────────────────────────────────────────────────────

func TestRunBacktestOrg_NotFound(t *testing.T) {
	dir := t.TempDir()
	orgReportsDir = dir
	defer func() { orgReportsDir = "" }()

	err := CMDBacktestOrg.RunE(CMDBacktestOrg, []string{"nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunBacktestOrg_Found(t *testing.T) {
	dir := t.TempDir()
	orgReportsDir = dir
	defer func() { orgReportsDir = "" }()

	content := "* My Backtest\n** Results\n- trades: 42\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my-run.org"), []byte(content), 0o644))

	var buf bytes.Buffer
	CMDBacktestOrg.SetOut(&buf)

	err := CMDBacktestOrg.RunE(CMDBacktestOrg, []string{"my-run"})
	require.NoError(t, err)
	assert.Equal(t, content, buf.String())
}

func TestRunBacktestOrg_WithDotOrg(t *testing.T) {
	dir := t.TempDir()
	orgReportsDir = dir
	defer func() { orgReportsDir = "" }()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "my-run.org"), []byte("org content\n"), 0o644))

	var buf bytes.Buffer
	CMDBacktestOrg.SetOut(&buf)

	// Passing name with .org suffix should also work (service strips/adds as needed).
	err := CMDBacktestOrg.RunE(CMDBacktestOrg, []string{"my-run.org"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "org content")
}

// ── backtest configs ──────────────────────────────────────────────────────

func TestRunBacktestConfigs_Empty(t *testing.T) {
	dir := t.TempDir()
	configsDir = dir
	defer func() { configsDir = "" }()

	var buf bytes.Buffer
	CMDBacktestConfigs.SetOut(&buf)

	err := CMDBacktestConfigs.RunE(CMDBacktestConfigs, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No config files found")
}

func TestRunBacktestConfigs_ListsSorted(t *testing.T) {
	dir := t.TempDir()
	configsDir = dir
	defer func() { configsDir = "" }()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "z-run.yml"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a-run.yaml"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "m-run.json"), []byte("{}"), 0o644))

	var buf bytes.Buffer
	CMDBacktestConfigs.SetOut(&buf)

	err := CMDBacktestConfigs.RunE(CMDBacktestConfigs, nil)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "a-run.yaml")
	assert.Contains(t, out, "z-run.yml")
	assert.Contains(t, out, "3 config(s)")

	// Verify sort order: a before z.
	aIdx := strings.Index(out, "a-run")
	zIdx := strings.Index(out, "z-run")
	assert.Less(t, aIdx, zIdx, "configs should be sorted alphabetically")
}

// ── backtest candles (error paths only; happy path requires candle files) ──

func TestRunBacktestCandles_ReportNotFound(t *testing.T) {
	dir := t.TempDir()
	candlesReportsDir = dir
	defer func() { candlesReportsDir = "" }()

	err := CMDBacktestCandles.RunE(CMDBacktestCandles, []string{"nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
