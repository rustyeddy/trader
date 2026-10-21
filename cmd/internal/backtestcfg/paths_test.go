package backtestcfg

import (
	"os"
	"path/filepath"
	"strings"
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

// load resolves the config with an explicit environment. A nil env means "no
// environment", never the developer's: tests that want the process
// environment say so with t.Setenv and nil Options.Environ directly.
func load(t *testing.T, path string, env []string, overrides map[string]string) RunConfig {
	t.Helper()
	if env == nil {
		env = []string{}
	}
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

	out, err := LoadOutput([]string{}, p, "", "")
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
	out, err := LoadOutput([]string{}, p, "", "")
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

	out, err := LoadOutput([]string{}, p, "flag/out", "table")
	require.NoError(t, err)
	assert.Equal(t, Output{Dir: "flag/out", Format: "table"}, out)

	out, err = LoadOutput([]string{"TRADER_BACKTEST_OUTPUT_DIR=env/out", "TRADER_BACKTEST_OUTPUT_FORMAT=table"}, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, Output{Dir: "env/out", Format: "table"}, out)
}

// Without a config file nothing is resolved against a directory.
func TestNoConfigFileLeavesPathsAlone(t *testing.T) {
	out, err := LoadOutput([]string{}, "", "", "")
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
	out, err := LoadOutput([]string{}, p, "", "")
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

	out, err := LoadOutput([]string{}, p, "out", "")
	require.NoError(t, err)
	assert.Equal(t, "out", out.Dir)
	out, err = LoadOutput([]string{"TRADER_BACKTEST_OUTPUT_DIR=out"}, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, "out", out.Dir)
}

// A nil Environ makes the loader read the process environment, so provenance
// must read it too: a variable that repeats the file's text is the caller's
// own path, not the file's.
func TestNilEnvironmentIsTheProcessEnvironmentForProvenanceToo(t *testing.T) {
	p := writeAt(t, t.TempDir(), pathsYAML)
	t.Setenv("TRADER_BACKTEST_OUTPUT_DIR", "out")
	t.Setenv("TRADER_MARKET_DATA_RAW_ROOT", "../data/raw")
	t.Setenv("TRADER_MARKET_DATA_STORE_ROOT", "store")

	out, err := LoadOutput(nil, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, "out", out.Dir)

	cfg, err := LoadRunConfig(config.Options{EnvPrefix: "TRADER", FilePath: p})
	require.NoError(t, err)
	assert.Equal(t, "../data/raw", cfg.Market.DataRawRoot)
	assert.Equal(t, "store", cfg.Market.DataStoreRoot)

	// An explicitly empty environment is an empty environment, not the process's.
	out, err = LoadOutput([]string{}, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(filepath.Dir(p), "out"), out.Dir)
}

const isolatedYAML = "isolate_environment: true\n" + pathsYAML

var hostileEnv = []string{
	"TRADER_MARKET_DATA_RAW_ROOT=/hostile/raw",
	"TRADER_MARKET_DATA_STORE_ROOT=/srv/trading/data/canonical",
	"TRADER_BACKTEST_OUTPUT_DIR=/hostile/out",
	"TRADER_BACKTEST_OUTPUT_FORMAT=table",
	"TRADER_BACKTEST_CURRENCY=EUR",
	"TRADER_STRATEGY_FAST_PERIOD=7",
}

// A document that opts out of the environment means what it says: nothing
// exported can redirect it.
func TestIsolatedDocumentIgnoresTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	p := writeAt(t, dir, isolatedYAML)

	cfg := load(t, p, hostileEnv, nil)
	assert.Equal(t, filepath.Join(filepath.Dir(dir), "data", "raw"), cfg.Market.DataRawRoot)
	assert.Equal(t, filepath.Join(dir, "store"), cfg.Market.DataStoreRoot)
	assert.NotEqual(t, "EUR", cfg.Backtest.Currency)
	assert.NotEqual(t, 7, cfg.Strategy.FastPeriod)

	out, err := LoadOutput(hostileEnv, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, Output{Dir: filepath.Join(dir, "out"), Format: "json"}, out)

	// The same holds when the process environment is the hostile one.
	for _, kv := range hostileEnv {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	cfg, err = LoadRunConfig(config.Options{EnvPrefix: "TRADER", FilePath: p})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "store"), cfg.Market.DataStoreRoot)
	out, err = LoadOutput(nil, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "out"), out.Dir)
}

// Isolation is opt-in per document. Without the key, an environment variable
// still overrides the file, exactly as before.
func TestOrdinaryDocumentStillLetsTheEnvironmentOverride(t *testing.T) {
	p := writeAt(t, t.TempDir(), pathsYAML)
	cfg := load(t, p, hostileEnv, nil)
	assert.Equal(t, "/hostile/raw", cfg.Market.DataRawRoot)
	assert.Equal(t, "/srv/trading/data/canonical", cfg.Market.DataStoreRoot)
	out, err := LoadOutput(hostileEnv, p, "", "")
	require.NoError(t, err)
	assert.Equal(t, Output{Dir: "/hostile/out", Format: "table"}, out)
}

// Isolation drops the environment, not the command line: a flag is typed for
// this run.
func TestIsolatedDocumentStillTakesFlags(t *testing.T) {
	p := writeAt(t, t.TempDir(), isolatedYAML)
	cfg := load(t, p, hostileEnv, map[string]string{"data-raw-root": "flag/raw"})
	assert.Equal(t, "flag/raw", cfg.Market.DataRawRoot)
	out, err := LoadOutput(hostileEnv, p, "flag/out", "table")
	require.NoError(t, err)
	assert.Equal(t, Output{Dir: "flag/out", Format: "table"}, out)
}

func TestIsolationMustBeABoolean(t *testing.T) {
	for _, v := range []string{`"true"`, "1", "yes-please"} {
		p := writeAt(t, t.TempDir(), "isolate_environment: "+v+"\n"+pathsYAML)
		_, err := LoadRunConfig(config.Options{EnvPrefix: "TRADER", Environ: []string{}, FilePath: p})
		require.Error(t, err, v)
		assert.Contains(t, err.Error(), "isolate_environment must be true or false", v)
		_, err = LoadOutput([]string{}, p, "", "")
		require.Error(t, err, v)
	}
}
