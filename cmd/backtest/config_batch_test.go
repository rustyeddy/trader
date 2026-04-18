package backtest

import (
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectConfigPaths_File(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "single.yml")
	require.NoError(t, osWriteFile(cfgPath, "version: 1\nruns: []\n"))

	paths, err := collectConfigPaths(cfgPath)
	require.NoError(t, err)
	require.Len(t, paths, 1)
	assert.Equal(t, cfgPath, paths[0])
}

func TestCollectConfigPaths_DirectorySortedYMLOnly(t *testing.T) {
	tmp := t.TempDir()
	bPath := filepath.Join(tmp, "b.yml")
	aPath := filepath.Join(tmp, "a.yml")
	ignored := filepath.Join(tmp, "ignored.yaml")

	require.NoError(t, osWriteFile(bPath, "version: 1\nruns: []\n"))
	require.NoError(t, osWriteFile(aPath, "version: 1\nruns: []\n"))
	require.NoError(t, osWriteFile(ignored, "version: 1\nruns: []\n"))

	paths, err := collectConfigPaths(tmp)
	require.NoError(t, err)
	assert.Equal(t, []string{aPath, bPath}, paths)
}

func TestCollectConfigPaths_DirectoryNoYML(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, osWriteFile(filepath.Join(tmp, "cfg.yaml"), "version: 1\nruns: []\n"))

	_, err := collectConfigPaths(tmp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contains no *.yml files")
}

func TestReportOutputPath(t *testing.T) {
	run := trader.BacktestRun{
		Name: "EMA Cross Baseline",
		Kind: "ema-cross",
	}

	got := reportOutputPath("reports", "/tmp/configs/eurusd-suite.yml", run)
	want := filepath.Join("reports", "eurusd-suite", "ema-cross-baseline--ema-cross.txt")
	assert.Equal(t, want, got)
}

func TestSlug(t *testing.T) {
	assert.Equal(t, "ema-cross-baseline", slug("EMA Cross Baseline"))
	assert.Equal(t, "alpha-beta", slug("Alpha/Beta"))
	assert.Equal(t, "item", slug("***"))
}

func osWriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
