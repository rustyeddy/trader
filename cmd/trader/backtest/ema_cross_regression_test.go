package backtest_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
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
	cmd := cmdbacktest.New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"run", "--config", configPath})
	require.NoError(t, cmd.Execute())
	return out.Bytes()
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
	cfg, err := backtestcfg.LoadRunConfig(config.Options{EnvPrefix: "TRADER", Environ: []string{}, FilePath: configPath})
	require.NoError(t, err)

	want, err := filepath.Abs(rawFixtureRoot)
	require.NoError(t, err)
	gotRaw, err := filepath.Abs(cfg.Market.DataRawRoot)
	require.NoError(t, err)
	assert.Equal(t, want, gotRaw, "data_raw_root must resolve to the shared fixture")
	assert.Empty(t, cfg.Market.DataStoreRoot, "an empty store root means a fresh temporary store; a value or the default would persist state")

	out, err := backtestcfg.LoadOutput([]string{}, configPath, "", "")
	require.NoError(t, err)
	assert.Equal(t, "json", out.Format)
	assert.Equal(t, filepath.Join(regressionDir, "out"), out.Dir)

	for _, p := range []string{cfg.Market.DataRawRoot, cfg.Market.DataStoreRoot, out.Dir} {
		assert.False(t, strings.HasPrefix(p, "/srv"), "%q must not point into /srv/trading", p)
		assert.False(t, filepath.IsAbs(p) && !strings.HasPrefix(p, filepath.Dir(filepath.Dir(want))), "%q is machine-specific", p)
	}
}

// Running the regression leaves the source tree as it found it.
// hostileEnvironment is what a developer might have exported: a different raw
// root, the production canonical store, another output directory and format.
// None of it may reach the regression, and it is not removed for the test: the
// config's own isolation is what is being proved.
func hostileEnvironment(t *testing.T) (rawRoot, storeRoot, outDir string) {
	t.Helper()
	base := t.TempDir()
	rawRoot, storeRoot, outDir = filepath.Join(base, "hostile-raw"), filepath.Join(base, "hostile-store"), filepath.Join(base, "hostile-out")
	t.Setenv("TRADER_MARKET_DATA_RAW_ROOT", rawRoot)
	t.Setenv("TRADER_MARKET_DATA_STORE_ROOT", storeRoot)
	t.Setenv("TRADER_BACKTEST_OUTPUT_DIR", outDir)
	t.Setenv("TRADER_BACKTEST_OUTPUT_FORMAT", "table")
	t.Setenv("TRADER_BACKTEST_INITIAL_MARGIN_RATIO", "1")
	t.Setenv("TRADER_STRATEGY_FAST_PERIOD", "7")
	return rawRoot, storeRoot, outDir
}

func assertUntouched(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		_, err := os.Stat(p)
		assert.True(t, os.IsNotExist(err), "%s was created: the environment reached the regression", p)
	}
}

// The advertised invocation is "backtest run --config <file>" and nothing
// else, so it must give the same results, and touch only the same places,
// with hostile TRADER_* variables exported.
func TestEMACrossRegression_HostileEnvironmentDoesNotReachTheRun(t *testing.T) {
	raw, store, out := hostileEnvironment(t)
	got := stable(t, runRegression(t, stagedRegression(t)))
	want, err := os.ReadFile(filepath.Join(regressionDir, "expected.json"))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
	assertUntouched(t, raw, store, out)
}

// The same through the real binary, the way a developer runs it, so no
// in-process shortcut can stand in for the advertised command.
func TestEMACrossRegression_RealCommandWithHostileEnvironment(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the real command")
	}
	raw, store, out := hostileEnvironment(t)
	staged := stagedRegression(t)

	cmd := exec.Command("go", "run", "./cmd/trader", "backtest", "run", "--config", staged)
	cmd.Dir = "../../.."
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	require.NoError(t, cmd.Run(), stderr.String())

	want, err := os.ReadFile(filepath.Join(regressionDir, "expected.json"))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(stable(t, stdout.Bytes())))
	assertUntouched(t, raw, store, out)
}

func TestEMACrossRegression_DoesNotWriteIntoTheSourceTree(t *testing.T) {
	before, err := os.ReadDir(regressionDir)
	require.NoError(t, err)
	runRegression(t, stagedRegression(t))
	after, err := os.ReadDir(regressionDir)
	require.NoError(t, err)
	assert.Equal(t, len(before), len(after), "the regression wrote into %s", regressionDir)
}
