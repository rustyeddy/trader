package backtest_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
)

// flipFlopPath is the path to the examples/strategysdk-minimal
// fixture binary — the same real, out-of-tree strategysdk-based
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

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

func runTestMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "flipflop-bin-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "backtest: building strategysdk-minimal test fixture:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	flipFlopPath = filepath.Join(dir, "flipflop")
	build := exec.Command("go", "build", "-o", flipFlopPath,
		"github.com/rustyeddy/trader/examples/strategysdk-minimal")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "backtest: building strategysdk-minimal test fixture:", err)
		return 1
	}

	return m.Run()
}

// TestVerticalSlice_RunWithStrategyExec is issue #382's own core
// acceptance path: "run" launches a real out-of-tree strategy
// executable (examples/strategysdk-minimal's flip-flop strategy —
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
	// strategysdk-minimal/main.go) — a manifest reporting anything else
	// would mean the external adapter's Handshake-received Descriptor
	// never actually reached backtest.Manifest.
	assert.Equal(t, "flipflop", doc.Run.StrategyName)

	// flat->long->flat over four bars closes at least one round-trip
	// trade — proof the M4 pipeline and simulator actually filled
	// orders the external process itself decided to submit, not merely
	// that the process launched and exited cleanly.
	assert.NotEmpty(t, doc.ClosedTrades, "expected at least one closed trade from the external strategy's own enter/exit cycle:\n%s", runOutput)
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
// clear error from Launch itself, rather than a confusing failure
// deeper in the pipeline.
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
	assert.Contains(t, err.Error(), "launching --strategy-exec")
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
