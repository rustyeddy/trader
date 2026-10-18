package backtest_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/cmd/internal/backtestcfg"
	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
	"github.com/rustyeddy/trader/internal/config"
)

var update = flag.Bool("update", false, "rewrite testdata/backtest/ema-cross/expected.json after an approved behaviour change")

// regressionDir is the committed one-config EMA-cross regression (issue #542),
// relative to this package.
const regressionDir = "../../../testdata/backtest/ema-cross"

// stagedRegression copies the committed config.yml byte for byte into a
// temporary tree laid out like the repository's testdata, with the shared
// fixture copied in, so the run reads the real config and the real data but
// writes its snapshots into the temporary directory, never the source tree.
// It returns the staged config path.
func stagedRegression(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "testdata", "backtest", "ema-cross")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	cfg, err := os.ReadFile(filepath.Join(regressionDir, "config.yml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), cfg, 0o644))
	copyTree(t, rawFixtureAbs, filepath.Join(root, "testdata", "data", "oanda", "raw"))
	return filepath.Join(dir, "config.yml")
}

// copyTree copies the small fixture archive; the data reader does not follow
// a symlinked root, so linking it in would look like missing data.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	}))
}

// runRegression runs the regression exactly as the developer command does:
// "backtest run --config <file>" and nothing else.
func runRegression(t *testing.T, configPath string) []byte {
	t.Helper()
	isolateEnvironment(t)
	cmd := cmdbacktest.New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"run", "--config", configPath})
	require.NoError(t, cmd.Execute())
	return out.Bytes()
}

// isolateEnvironment removes every TRADER_* variable for the test, so a
// developer's own settings (a raw root, a store root, an output directory)
// cannot change what the regression reads or writes. They come back afterwards.
func isolateEnvironment(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, "TRADER_") {
			continue
		}
		require.NoError(t, os.Unsetenv(name))
		t.Cleanup(func() { _ = os.Setenv(name, value) })
	}
}

// stable projects a run report onto the figures that must not change: what
// ran, what traded and what it earned. Run and account IDs, the config digest
// and the dataset revision hash are identifiers of one run or one build, not
// outcomes, so they are left out rather than compared as if deterministic.
func stable(t *testing.T, report []byte) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(report))
	dec.UseNumber()
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc))

	run := doc["run"].(map[string]any)
	delete(run, "run_id")
	delete(run, "config_digest")
	for _, d := range doc["dataset"].([]any) {
		delete(d.(map[string]any), "revision")
	}
	delete(doc["account"].(map[string]any), "account_id")

	out, err := json.MarshalIndent(doc, "", "  ")
	require.NoError(t, err)
	return append(out, '\n')
}

// TestEMACrossRegression_MatchesExpected is the safety net for refactoring
// the backtest command and for changing financial execution (#539, #531): the
// committed config, run with --config alone, must reproduce the committed
// expected.json. Change expected.json only deliberately, with -update, after
// an approved behaviour change (docs/arch/testing.md).
func TestEMACrossRegression_MatchesExpected(t *testing.T) {
	got := stable(t, runRegression(t, stagedRegression(t)))

	path := filepath.Join(regressionDir, "expected.json")
	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to create %s", path)
	if d := lineDiff(string(want), string(got)); d != "" {
		t.Fatalf("the EMA-cross baseline changed (- expected.json, + this run):\n%s\nif this is an approved behaviour change, rerun with -update and review the diff", d)
	}
}

// lineDiff lists the first lines that differ, so a failure says which figure
// moved instead of dumping two whole documents.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	shown := 0
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		if shown++; shown > 8 {
			b.WriteString("  ...\n")
			break
		}
		fmt.Fprintf(&b, "  line %d:\n    - %s\n    + %s\n", i+1, strings.TrimSpace(wl), strings.TrimSpace(gl))
	}
	return b.String()
}

// The baseline must actually say what the issue requires, so a regenerated
// file that lost it cannot pass quietly.
func TestEMACrossRegression_BaselineCoversTheRequiredFacts(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(regressionDir, "expected.json"))
	require.NoError(t, err)
	var doc struct {
		Run struct {
			StrategyName string `json:"strategy_name"`
		} `json:"run"`
		ClosedTrades []struct {
			Side string `json:"side"`
		} `json:"closed_trades"`
		OpenTrades []struct {
			Side string `json:"side"`
		} `json:"open_trades"`
		Account struct {
			Equity struct{ Amount string } `json:"equity"`
		} `json:"account"`
		EquityCurve []any `json:"equity_curve"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "ema-cross", doc.Run.StrategyName)
	require.Len(t, doc.ClosedTrades, 1)
	assert.Equal(t, "long", doc.ClosedTrades[0].Side)
	require.Len(t, doc.OpenTrades, 1)
	assert.Equal(t, "short", doc.OpenTrades[0].Side)
	assert.NotEmpty(t, doc.Account.Equity.Amount)
	assert.NotEmpty(t, doc.EquityCurve)
	assert.NotContains(t, string(raw), "run_id")
}

// The regression is a development test: it must stay inside the repository.
// The config names its fixture relative to itself, selects a fresh temporary
// canonical store rather than the production default, and writes snapshots
// to a directory beside itself.
func TestEMACrossRegression_ConfigIsSelfContained(t *testing.T) {
	configPath := filepath.Join(regressionDir, "config.yml")
	cfg, err := backtestcfg.LoadRunConfig(config.Options{EnvPrefix: "TRADER", Environ: nil, FilePath: configPath})
	require.NoError(t, err)

	want, err := filepath.Abs(rawFixtureRoot)
	require.NoError(t, err)
	gotRaw, err := filepath.Abs(cfg.Market.DataRawRoot)
	require.NoError(t, err)
	assert.Equal(t, want, gotRaw, "data_raw_root must resolve to the shared fixture")
	assert.Empty(t, cfg.Market.DataStoreRoot, "an empty store root means a fresh temporary store; a value or the default would persist state")

	out, err := backtestcfg.LoadOutput(nil, configPath, "", "")
	require.NoError(t, err)
	assert.Equal(t, "json", out.Format)
	assert.Equal(t, filepath.Join(regressionDir, "out"), out.Dir)

	for _, p := range []string{cfg.Market.DataRawRoot, cfg.Market.DataStoreRoot, out.Dir} {
		assert.False(t, strings.HasPrefix(p, "/srv"), "%q must not point into /srv/trading", p)
		assert.False(t, filepath.IsAbs(p) && !strings.HasPrefix(p, filepath.Dir(filepath.Dir(want))), "%q is machine-specific", p)
	}
}

// Running the regression leaves the source tree as it found it.
// A developer's TRADER_* settings must not leak into the regression.
func TestEMACrossRegression_IgnoresTheDevelopersEnvironment(t *testing.T) {
	want := stable(t, runRegression(t, stagedRegression(t)))
	t.Setenv("TRADER_MARKET_DATA_RAW_ROOT", "/nonexistent")
	t.Setenv("TRADER_MARKET_DATA_STORE_ROOT", "/srv/trading/data/canonical")
	t.Setenv("TRADER_BACKTEST_OUTPUT_FORMAT", "table")
	assert.Equal(t, string(want), string(stable(t, runRegression(t, stagedRegression(t)))))
}

func TestEMACrossRegression_DoesNotWriteIntoTheSourceTree(t *testing.T) {
	before, err := os.ReadDir(regressionDir)
	require.NoError(t, err)
	runRegression(t, stagedRegression(t))
	after, err := os.ReadDir(regressionDir)
	require.NoError(t, err)
	assert.Equal(t, len(before), len(after), "the regression wrote into %s", regressionDir)
}
