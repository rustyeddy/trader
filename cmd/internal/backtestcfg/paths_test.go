package backtestcfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/config"
)

// pathsYAML names every path-valued key relatively.
const pathsYAML = `
kind: backtest
market:
  symbols: EURUSD
  from: 2024-03-04T00:00:00Z
  to: 2024-03-04T14:00:00Z
  data_raw_root: ../data/raw
  data_store_root: store
backtest:
  adverse_distance: 0.01
  output_dir: out
  output_format: json
`

func writeAt(t *testing.T, dir, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, "run.yml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

func load(t *testing.T, path string, env []string, overrides map[string]string) RunConfig {
	t.Helper()
	cfg, err := LoadRunConfig(config.Options{EnvPrefix: "TRADER", Environ: env, FilePath: path, Overrides: overrides})
	require.NoError(t, err)
	return cfg
}

// A path written in the config file is relative to that file, whatever the
// working directory, so a checked-in config runs the same everywhere.
func TestFilePathsAreRelativeToTheConfigFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b")
	p := writeAt(t, dir, pathsYAML)

	cfg := load(t, p, nil, nil)
	assert.Equal(t, filepath.Join(root, "a", "data", "raw"), cfg.Market.DataRawRoot)
	assert.Equal(t, filepath.Join(dir, "store"), cfg.Market.DataStoreRoot)

	out, err := LoadOutput(nil, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "out"), out.Dir)
	assert.Equal(t, "json", out.Format)
}

func TestAbsolutePathsAreLeftAlone(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "elsewhere")
	p := writeAt(t, root, "kind: backtest\nmarket:\n  symbols: EURUSD\n  from: 2024-03-04T00:00:00Z\n  to: 2024-03-04T14:00:00Z\n  data_raw_root: "+abs+"\nbacktest:\n  adverse_distance: 0.01\n  output_dir: "+abs+"\n")
	cfg := load(t, p, nil, nil)
	assert.Equal(t, abs, cfg.Market.DataRawRoot)
	out, err := LoadOutput(nil, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, abs, out.Dir)
}

// Only the file's own values move. A flag or an environment variable names a
// path the caller typed, relative to where they typed it.
func TestFlagAndEnvironmentPathsStayWorkingDirectoryRelative(t *testing.T) {
	p := writeAt(t, t.TempDir(), pathsYAML)

	cfg := load(t, p, nil, map[string]string{"data-raw-root": "flag/raw", "data-store-root": "flag/store"})
	assert.Equal(t, "flag/raw", cfg.Market.DataRawRoot)
	assert.Equal(t, "flag/store", cfg.Market.DataStoreRoot)

	cfg = load(t, p, []string{"TRADER_MARKET_DATA_RAW_ROOT=env/raw", "TRADER_MARKET_DATA_STORE_ROOT=env/store"}, nil)
	assert.Equal(t, "env/raw", cfg.Market.DataRawRoot)
	assert.Equal(t, "env/store", cfg.Market.DataStoreRoot)

	out, err := LoadOutput(nil, p, "flag/out", "table")
	require.NoError(t, err)
	assert.Equal(t, Output{Dir: "flag/out", Format: "table"}, out)

	out, err = LoadOutput([]string{"TRADER_BACKTEST_OUTPUT_DIR=env/out", "TRADER_BACKTEST_OUTPUT_FORMAT=table"}, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, Output{Dir: "env/out", Format: "table"}, out)
}

// Without a config file nothing is resolved against a directory.
func TestNoConfigFileLeavesPathsAlone(t *testing.T) {
	out, err := LoadOutput(nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, Output{Dir: DefaultOutputDir, Format: DefaultOutputFormat}, out)
}

// An explicitly empty store root is the documented way to ask for a fresh
// temporary store; resolving it against the file would turn it into the
// file's own directory, and omitting it selects the production default.
func TestEmptyStoreRootStaysEmptyAndOmittedIsTheDefault(t *testing.T) {
	dir := t.TempDir()
	base := "kind: backtest\nmarket:\n  symbols: EURUSD\n  from: 2024-03-04T00:00:00Z\n  to: 2024-03-04T14:00:00Z\n"
	tail := "backtest:\n  adverse_distance: 0.01\n"

	cfg := load(t, writeAt(t, filepath.Join(dir, "empty"), base+"  data_store_root: \"\"\n"+tail), nil, nil)
	assert.Empty(t, cfg.Market.DataStoreRoot)

	cfg = load(t, writeAt(t, filepath.Join(dir, "omitted"), base+tail), nil, nil)
	assert.Equal(t, "/srv/trading/data/canonical", cfg.Market.DataStoreRoot, "the default is absolute and is not resolved against the file")
}

func TestOutputFormatDefaultsToTable(t *testing.T) {
	p := writeAt(t, t.TempDir(), "kind: backtest\nmarket:\n  symbols: EURUSD\n  from: 2024-03-04T00:00:00Z\n  to: 2024-03-04T14:00:00Z\nbacktest:\n  adverse_distance: 0.01\n")
	out, err := LoadOutput(nil, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, "table", out.Format)
	assert.Equal(t, DefaultOutputDir, out.Dir)
}

// A flag or variable that happens to repeat the file's text is still the
// caller's own path: where a value came from decides how it is read, not
// whether it equals the file's.
func TestFlagOrEnvironmentEqualToTheFileValueIsStillWorkingDirectoryRelative(t *testing.T) {
	p := writeAt(t, t.TempDir(), pathsYAML)

	cfg := load(t, p, nil, map[string]string{"data-raw-root": "../data/raw", "data-store-root": "store"})
	assert.Equal(t, "../data/raw", cfg.Market.DataRawRoot)
	assert.Equal(t, "store", cfg.Market.DataStoreRoot)

	cfg = load(t, p, []string{"TRADER_MARKET_DATA_RAW_ROOT=../data/raw", "TRADER_MARKET_DATA_STORE_ROOT=store"}, nil)
	assert.Equal(t, "../data/raw", cfg.Market.DataRawRoot)
	assert.Equal(t, "store", cfg.Market.DataStoreRoot)

	out, err := LoadOutput(nil, p, "out", "")
	require.NoError(t, err)
	assert.Equal(t, "out", out.Dir)
	out, err = LoadOutput([]string{"TRADER_BACKTEST_OUTPUT_DIR=out"}, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, "out", out.Dir)
}
