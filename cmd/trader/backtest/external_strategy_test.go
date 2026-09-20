package backtest_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
)

// flipFlopPath is the path to the examples/sdk-minimal
// fixture binary — the same real, out-of-tree sdk-based
// strategy issue #381 shipped as its own "one minimal example binary
// compiles and runs" acceptance criterion — built once in TestMain and
// reused by every test in this file. Using this real example, rather
// than a hand-rolled fake guest, proves --strategy-exec's own
// composition-root wiring (Launch -> Strategy() ->
// service/backtest.RunRequest.Strategy) against an executable that
// knows nothing about trader's own process beyond Strategy Protocol
// v1 (issue #382's own "integration test covers launch through the
// real backtest service path" acceptance criterion).
var flipFlopPath string

// fakeGuestPath is adapters/strategy/external's own fakeguest test
// fixture (cmd/internal/fakeguest), reused here for its
// FAKEGUEST_MODE=crash-after-handshake behavior: it completes a real
// Handshake and opens the Run stream (declaring zero Requirements,
// so Scheduler never needs another RPC once Start's own fire-and-
// forget SessionStart is sent), then os.Exit(1)s immediately —
// exactly the "external process crashes and the run must not still be
// reported as successful" scenario the review that added
// runWithExternalProcessMonitor asks for a regression test of.
var fakeGuestPath string

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

func runTestMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "backtest-strategy-bin-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "backtest: building test fixtures:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	flipFlopPath = filepath.Join(dir, "flipflop")
	if err := goBuild(flipFlopPath, "github.com/rustyeddy/trader/examples/sdk-minimal"); err != nil {
		fmt.Fprintln(os.Stderr, "backtest: building sdk-minimal test fixture:", err)
		return 1
	}

	fakeGuestPath = filepath.Join(dir, "fakeguest")
	if err := goBuild(fakeGuestPath, "github.com/rustyeddy/trader/cmd/internal/fakeguest"); err != nil {
		fmt.Fprintln(os.Stderr, "backtest: building fakeguest test fixture:", err)
		return 1
	}

	return m.Run()
}

func goBuild(out, pkg string) error {
	build := exec.Command("go", "build", "-o", out, pkg)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	return build.Run()
}

// TestVerticalSlice_RunWithStrategyExec is issue #382's own core
// acceptance path: "run" launches a real out-of-tree strategy
// executable (examples/sdk-minimal's flip-flop strategy —
// flat on its first EUR/USD H1 bar, long on the second, flat again on
// the third, and so on) over a real Unix-domain socket and Strategy
// Protocol v1 Handshake, drives it through the real M5
// pipeline/simulator exactly like any in-tree strategy, and persists
// a real report — proving the entire chain from CLI flag to a fill,
// not merely that the child process launched.
func TestVerticalSlice_RunWithStrategyExec(t *testing.T) {
	outputDir := t.TempDir()

	runCmd := cmdbacktest.New()
	var runOut bytes.Buffer
	runCmd.SetOut(&runOut)
	runCmd.SetArgs([]string{
		"run",
		"--strategy-exec", flipFlopPath,
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--starting-cash", "10000",
		"--currency", "USD",
		"--risk-fraction", "0.01",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--data-store-root", t.TempDir(),
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, runCmd.Execute())

	runOutput := runOut.String()
	require.NotEmpty(t, runOutput)

	var doc struct {
		Run struct {
			RunID        string `json:"run_id"`
			StrategyName string `json:"strategy_name"`
		} `json:"run"`
		ClosedTrades []json.RawMessage `json:"closed_trades"`
		OpenTrades   []json.RawMessage `json:"open_trades"`
	}
	require.NoError(t, json.Unmarshal([]byte(runOutput), &doc))
	require.NotEmpty(t, doc.Run.RunID)

	// flipflop's own Describe() names it "flipflop" (examples/
	// sdk-minimal/main.go) — a manifest reporting anything else
	// would mean the external adapter's Handshake-received Descriptor
	// never actually reached backtest.Manifest.
	assert.Equal(t, "flipflop", doc.Run.StrategyName)

	// flat->long->flat over four bars closes at least one round-trip
	// trade — proof the M4 pipeline and simulator actually filled
	// orders the external process itself decided to submit, not merely
	// that the process launched and exited cleanly.
	assert.NotEmpty(t, doc.ClosedTrades, "expected at least one closed trade from the external strategy's own enter/exit cycle:\n%s", runOutput)
}

// TestVerticalSlice_RunWithStrategyExec_RecordsProvenance is issue
// #385's own core acceptance path: an external run's persisted
// manifest carries unambiguous strategy/protocol/config provenance —
// execution mode, the guest's own Descriptor identity, the negotiated
// Strategy Protocol version, transport kind, the launched
// executable's resolved absolute path and content digest, and the
// resolved config path — read back from the same real "trader
// backtest run" JSON output every other vertical-slice test in this
// file already exercises, not a private shortcut into run.go's own
// internals.
func TestVerticalSlice_RunWithStrategyExec_RecordsProvenance(t *testing.T) {
	outputDir := t.TempDir()

	configPath := filepath.Join(t.TempDir(), "flipflop.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{}`), 0o600))

	runCmd := cmdbacktest.New()
	var runOut bytes.Buffer
	runCmd.SetOut(&runOut)
	runCmd.SetArgs([]string{
		"run",
		"--strategy-exec", flipFlopPath,
		"--strategy-config", configPath,
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--data-store-root", t.TempDir(),
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, runCmd.Execute())

	var doc struct {
		Run struct {
			StrategyName       string          `json:"strategy_name"`
			StrategyVersion    string          `json:"strategy_version"`
			StrategyParameters json.RawMessage `json:"strategy_parameters"`
		} `json:"run"`
	}
	require.NoError(t, json.Unmarshal(runOut.Bytes(), &doc))

	var params struct {
		Mode            string   `json:"mode"`
		StrategyName    string   `json:"strategy_name"`
		StrategyVersion string   `json:"strategy_version"`
		ProtocolVersion string   `json:"protocol_version"`
		Transport       string   `json:"transport"`
		Exec            string   `json:"exec"`
		ExecDigest      string   `json:"exec_digest"`
		Args            []string `json:"args"`
		Config          string   `json:"config"`
		ConfigDigest    string   `json:"config_digest"`
	}
	require.NoError(t, json.Unmarshal(doc.Run.StrategyParameters, &params))

	assert.Equal(t, "external", params.Mode)
	assert.Equal(t, "flipflop", params.StrategyName)
	assert.Equal(t, "flipflop", doc.Run.StrategyName, "Manifest.StrategyName must agree with the provenance recorded in StrategyParameters")
	assert.Equal(t, "0.1.0", params.StrategyVersion)
	assert.Equal(t, "0.1.0", doc.Run.StrategyVersion)
	assert.Equal(t, "v1", params.ProtocolVersion)
	assert.Equal(t, "unix", params.Transport)
	assert.True(t, filepath.IsAbs(params.Exec), "exec path must be absolute, got %q", params.Exec)
	assert.Equal(t, flipFlopPath, params.Exec)
	assert.True(t, strings.HasPrefix(params.ExecDigest, "sha256:"), "expected a sha256: content digest, got %q", params.ExecDigest)
	assert.Len(t, params.ExecDigest, len("sha256:")+64)
	assert.True(t, filepath.IsAbs(params.Config), "config path must be absolute, got %q", params.Config)

	// ConfigDigest must match the exact bytes actually at configPath —
	// verified against the fixture file's own real content, not just
	// checked for the right shape, so a regression that hashed the
	// wrong file (or nothing at all) would be caught (review finding).
	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	sum := sha256.Sum256(configBytes)
	assert.Equal(t, "sha256:"+hex.EncodeToString(sum[:]), params.ConfigDigest)

	// The ephemeral Unix-domain socket path Launch generates for this
	// specific run must never leak into recorded provenance (issue
	// #385's own explicit "no ... machine-specific ephemeral socket
	// paths ... as semantic identity" exclusion) — checked here against
	// the raw JSON, not just the typed fields above, so a future field
	// added to externalStrategyParams that happens to carry it would
	// still be caught.
	assert.NotContains(t, string(doc.Run.StrategyParameters), "trader-strategy-", "a run's own ephemeral socket directory name must never appear in recorded provenance")
}

// TestVerticalSlice_RunWithStrategyExec_ProcessCrashReportedAsFailure
// is the review regression for the "external process exits but the
// run is still reported as successful" finding: fakeguest's own
// crash-after-first-bar mode completes a real Handshake and Run
// (declaring one real EUR/USD H1 requirement), answers exactly the
// first of this range's four bars, then os.Exit(1)s immediately —
// with three further bars still to replay, Scheduler needs more RPCs
// from a guest that is already gone. "run" must surface this as a
// failure (via runWithExternalProcessMonitor or the resulting RPC
// error either way), not a clean report.
func TestVerticalSlice_RunWithStrategyExec_ProcessCrashReportedAsFailure(t *testing.T) {
	t.Setenv("FAKEGUEST_MODE", "crash-after-first-bar")

	runCmd := cmdbacktest.New()
	runCmd.SetArgs([]string{
		"run",
		"--strategy-exec", fakeGuestPath,
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--data-store-root", t.TempDir(),
		"--output-dir", t.TempDir(),
	})
	err := runCmd.Execute()
	require.Error(t, err, "a crashed external strategy process must not let the run be reported as successful")
}

// TestRun_StrategyExecAndConfigMutuallyExclusive proves the invalid
// combination fails before any process is launched or data published
// (issue #382's own acceptance criterion), not partway through the run.
func TestRun_StrategyExecAndConfigMutuallyExclusive(t *testing.T) {
	runCmd := cmdbacktest.New()
	runCmd.SetArgs([]string{
		"run",
		"--strategy-exec", "/does/not/matter",
		"--config", "/does/not/matter/either.yaml",
		"--symbol", "EURUSD",
		"--data-raw-root", "testdata/raw/oanda",
	})
	err := runCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--strategy-exec")
	assert.Contains(t, err.Error(), "--config")
}

// TestRun_StrategyArgsWithoutStrategyExecRejected and
// TestRun_StrategyConfigWithoutStrategyExecRejected prove the two
// dependent flags fail clearly rather than being silently ignored
// when --strategy-exec is absent.
func TestRun_StrategyArgsWithoutStrategyExecRejected(t *testing.T) {
	runCmd := cmdbacktest.New()
	runCmd.SetArgs([]string{
		"run",
		"--strategy-args", "--verbose",
		"--symbol", "EURUSD",
		"--data-raw-root", "testdata/raw/oanda",
	})
	err := runCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--strategy-args")
	assert.Contains(t, err.Error(), "--strategy-exec")
}

// TestRun_StrategyExecLaunchFailureReportedClearly proves a
// --strategy-exec pointing at a nonexistent executable fails with a
// clear error, rather than a confusing failure deeper in the
// pipeline. This now fails even before Launch is ever called (issue
// #385): the executable's own content digest is computed up front,
// for provenance, so a missing file is caught there — strictly better
// than failing inside Launch, since no child process is ever even
// attempted.
func TestRun_StrategyExecLaunchFailureReportedClearly(t *testing.T) {
	runCmd := cmdbacktest.New()
	runCmd.SetArgs([]string{
		"run",
		"--strategy-exec", filepath.Join(t.TempDir(), "does-not-exist"),
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--data-store-root", t.TempDir(),
		"--output-dir", t.TempDir(),
	})
	err := runCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}

func TestRun_StrategyConfigWithoutStrategyExecRejected(t *testing.T) {
	runCmd := cmdbacktest.New()
	runCmd.SetArgs([]string{
		"run",
		"--strategy-config", "strategy.yaml",
		"--symbol", "EURUSD",
		"--data-raw-root", "testdata/raw/oanda",
	})
	err := runCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--strategy-config")
	assert.Contains(t, err.Error(), "--strategy-exec")
}
