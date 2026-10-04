package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
)

// cliFixtureRaw is the backtest CLI's committed EURUSD raw fixture.
const cliFixtureRaw = "../trader/backtest/testdata/raw/oanda"

// backtestServer serves trader-mcp over storeRoot, the CLI's raw fixture,
// and outputDir, returning a client session.
func backtestServer(t *testing.T, storeRoot, outputDir string, args ...string) *mcp.ClientSession {
	t.Helper()
	env, _ := baseEnv(t)
	for i, kv := range env {
		switch {
		case strings.HasPrefix(kv, "TRADER_STORE_ROOT="):
			env[i] = "TRADER_STORE_ROOT=" + storeRoot
		case strings.HasPrefix(kv, "TRADER_RAW_ROOT="):
			env[i] = "TRADER_RAW_ROOT=" + cliFixtureRaw
		}
	}
	env = append(env, "TRADER_BACKTEST_OUTPUT_DIR="+outputDir)
	srv, closer, err := build(args, env, &bytes.Buffer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil).Connect(ctx, clientT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	if res.IsError {
		return res, nil
	}
	out, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	return res, out
}

func toolText(res *mcp.CallToolResult) string {
	return res.Content[0].(*mcp.TextContent).Text
}

// demoRun is one backtest's inputs, as MCP arguments.
var demoRun = map[string]any{
	"symbols": []string{"EURUSD"}, "interval": "H1",
	"from": "2024-01-08T00:00:00Z", "to": "2024-01-08T04:00:00Z",
	"adverse_distance": "0.01000", "initial_margin_ratio": "0.25",
}

// runCLI runs the same backtest as demoRun through `trader backtest run`
// and returns its JSON report.
func runCLI(t *testing.T, storeRoot, outputDir string) map[string]any {
	t.Helper()
	cmd := cmdbacktest.New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"run", "--symbol", "EURUSD", "--interval", "H1",
		"--from", "2024-01-08T00:00:00Z", "--to", "2024-01-08T04:00:00Z",
		"--adverse-distance", "0.01000", "--initial-margin-ratio", "0.25",
		"--data-raw-root", cliFixtureRaw, "--data-store-root", storeRoot,
		"--output-dir", outputDir, "--format", "json",
	})
	require.NoError(t, cmd.Execute())
	var rep map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	return rep
}

// withoutRunIdentifiers returns rep without the opaque identifiers each
// run generates fresh — run.run_id and account.account_id — the only
// fields that differ between two runs of the same inputs (as ADR-041's
// determinism suite normalizes them).
func withoutRunIdentifiers(rep map[string]any) map[string]any {
	without := func(section, key string) map[string]any {
		out := map[string]any{}
		for k, v := range rep[section].(map[string]any) {
			if k != key {
				out[k] = v
			}
		}
		return out
	}
	out := map[string]any{}
	for k, v := range rep {
		out[k] = v
	}
	out["run"] = without("run", "run_id")
	out["account"] = without("account", "account_id")
	return out
}

// TestRunBacktest_MatchesCLI is #437's acceptance criterion: over one
// shared data store, an MCP run and a CLI run of the same inputs produce
// the same config digest and report, and each transport reads the
// other's stored run.
func TestRunBacktest_MatchesCLI(t *testing.T) {
	storeRoot, outputDir := t.TempDir(), t.TempDir()
	cliReport := runCLI(t, storeRoot, outputDir) // builds the canonical data

	cs := backtestServer(t, storeRoot, outputDir) // no --allow-writes: the data must be current
	res, out := callTool(t, cs, "trader_run_backtest", demoRun)
	require.False(t, res.IsError, toolText(res))

	cliRun := cliReport["run"].(map[string]any)
	assert.Equal(t, cliRun["config_digest"], out["config_digest"], "same inputs, same config digest")
	assert.NotEqual(t, cliRun["run_id"], out["run_id"])
	summary := out["summary"].(map[string]any)
	for _, section := range []string{"run", "dataset", "performance", "trade_stats", "margin", "account"} {
		assert.Contains(t, summary, section)
	}
	assert.NotContains(t, summary, "equity_curve", "the summary leaves out the bulky sections")
	assert.Equal(t, float64(1), summary["open_trade_count"])

	res, result := callTool(t, cs, "trader_backtest_result", map[string]any{"run_id": out["run_id"]})
	require.False(t, res.IsError, toolText(res))
	assert.Equal(t, withoutRunIdentifiers(cliReport), withoutRunIdentifiers(result["report"].(map[string]any)), "the same report")

	// The CLI reads the MCP run from the shared store, and MCP the CLI's.
	show := cmdbacktest.New()
	var shown bytes.Buffer
	show.SetOut(&shown)
	show.SetArgs([]string{"show", out["run_id"].(string), "--output-dir", outputDir, "--format", "json"})
	require.NoError(t, show.Execute())
	res, _ = callTool(t, cs, "trader_backtest_result", map[string]any{"run_id": cliRun["run_id"]})
	assert.False(t, res.IsError)
}

func TestRunBacktest_DataPreparationFollowsWritePolicy(t *testing.T) {
	storeRoot, outputDir := t.TempDir(), t.TempDir()

	readOnly := backtestServer(t, storeRoot, outputDir)
	res, _ := callTool(t, readOnly, "trader_run_backtest", demoRun)
	require.True(t, res.IsError)
	assert.Contains(t, toolText(res), "market data is not ready")
	assert.Contains(t, toolText(res), "EURUSD H1 2024-01 is missing")
	entries, err := os.ReadDir(storeRoot)
	require.NoError(t, err)
	assert.Empty(t, entries, "without --allow-writes, nothing is built")

	writable := backtestServer(t, storeRoot, outputDir, "--allow-writes")
	res, out := callTool(t, writable, "trader_run_backtest", demoRun)
	require.False(t, res.IsError, toolText(res))
	assert.FileExists(t, filepath.Join(outputDir, out["run_id"].(string)+".json"))
}

func TestRunBacktest_InvalidRequests(t *testing.T) {
	storeRoot, outputDir := t.TempDir(), t.TempDir()
	runCLI(t, storeRoot, outputDir)
	cs := backtestServer(t, storeRoot, outputDir)
	with := func(changes map[string]any) map[string]any {
		args := map[string]any{}
		for k, v := range demoRun {
			args[k] = v
		}
		for k, v := range changes {
			if v == nil {
				delete(args, k)
			} else {
				args[k] = v
			}
		}
		return args
	}
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"missing from":      {with(map[string]any{"from": nil}), `missing properties: ["from"]`},
		"unknown strategy":  {with(map[string]any{"strategy": map[string]any{"name": "typo"}}), `strategy.name "typo" is not registered`},
		"ema-cross, 2 syms": {with(map[string]any{"symbols": []string{"EURUSD", "GBPUSD"}, "strategy": map[string]any{"name": "ema-cross"}}), "exactly one instrument"},
		"bad interval":      {with(map[string]any{"interval": "H99"}), "invalid backtest request"},
		"unknown provider":  {with(map[string]any{"provider": "bloomberg"}), "unknown market-data provider"},
		"bad margin ratio":  {with(map[string]any{"initial_margin_ratio": "0"}), "initial_margin_ratio must be positive"},
		"duplicate symbols": {with(map[string]any{"symbols": []string{"EURUSD", "eurusd"}}), "duplicate"},
		"negative warmup":   {with(map[string]any{"warmup_bars": -1}), "warmup bars must not be negative"},
		"zero capital":      {with(map[string]any{"starting_capital": "0"}), "starting_capital must be positive"},
		"negative capital":  {with(map[string]any{"starting_capital": "-100"}), "starting_capital must be positive"},
		"zero fast period":  {with(map[string]any{"strategy": map[string]any{"name": "ema-cross", "fast_period": 0}}), "fast_period must be positive"},
		"zero slow period":  {with(map[string]any{"strategy": map[string]any{"name": "ema-cross", "slow_period": 0}}), "slow_period (0) must be greater"},
	} {
		t.Run(name, func(t *testing.T) {
			res, _ := callTool(t, cs, "trader_run_backtest", tc.args)
			require.True(t, res.IsError)
			assert.Contains(t, toolText(res), tc.want)
		})
	}
}

func TestBacktestResult_Failures(t *testing.T) {
	storeRoot, outputDir := t.TempDir(), t.TempDir()
	cliRun := runCLI(t, storeRoot, outputDir)["run"].(map[string]any)
	runID := cliRun["run_id"].(string)
	cs := backtestServer(t, storeRoot, outputDir)

	res, _ := callTool(t, cs, "trader_backtest_result", map[string]any{"run_id": "nonsense"})
	require.True(t, res.IsError)
	assert.Contains(t, toolText(res), "invalid backtest request")

	// A snapshot of a schema this build cannot read fails clearly.
	path := filepath.Join(outputDir, runID+".json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var snap map[string]any
	require.NoError(t, json.Unmarshal(raw, &snap))
	snap["schema_version"] = 99
	raw, err = json.Marshal(snap)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o644))
	res, _ = callTool(t, cs, "trader_backtest_result", map[string]any{"run_id": runID})
	require.True(t, res.IsError)
	assert.Contains(t, toolText(res), "schema version mismatch")
	assert.Contains(t, toolText(res), "version 99")
	assert.NotContains(t, toolText(res), outputDir, "no path reaches the client")

	require.NoError(t, os.Remove(path))
	res, _ = callTool(t, cs, "trader_backtest_result", map[string]any{"run_id": runID})
	require.True(t, res.IsError)
	assert.Contains(t, toolText(res), "no stored run with this id")
}

// TestRunBacktest_OmittedPeriodsTakeDefaults: omission, unlike an
// explicit 0, means the CLI's defaults (20/50).
func TestRunBacktest_OmittedPeriodsTakeDefaults(t *testing.T) {
	storeRoot, outputDir := t.TempDir(), t.TempDir()
	runCLI(t, storeRoot, outputDir)
	cs := backtestServer(t, storeRoot, outputDir)
	args := map[string]any{}
	for k, v := range demoRun {
		args[k] = v
	}
	args["strategy"] = map[string]any{"name": "ema-cross"}
	res, out := callTool(t, cs, "trader_run_backtest", args)
	require.False(t, res.IsError, toolText(res))
	params := out["summary"].(map[string]any)["run"].(map[string]any)["strategy_parameters"].(map[string]any)
	assert.Equal(t, float64(20), params["fast_period"])
	assert.Equal(t, float64(50), params["slow_period"])
}

// TestBacktestResult_ExternalRunOmitsLaunchDetails: a run the CLI made
// with --strategy-exec sits in the same store; MCP returns it without its
// machine-local executable/config paths and arguments (ADR-068).
func TestBacktestResult_ExternalRunOmitsLaunchDetails(t *testing.T) {
	storeRoot, outputDir := t.TempDir(), t.TempDir()
	runID := runCLI(t, storeRoot, outputDir)["run"].(map[string]any)["run_id"].(string)

	// Rewrite the stored run's parameters into what an external run records.
	path := filepath.Join(outputDir, runID+".json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var snap map[string]any
	require.NoError(t, json.Unmarshal(raw, &snap))
	snap["report"].(map[string]any)["run"].(map[string]any)["strategy_parameters"] = map[string]any{
		"mode": "external", "strategy_name": "guest", "exec": "/home/op/bin/guest", "exec_digest": "sha256:e1",
		"args": []string{"--token", "s3cret"}, "config": "/home/op/guest.yaml", "config_digest": "sha256:c1",
	}
	raw, err = json.Marshal(snap)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o644))

	cs := backtestServer(t, storeRoot, outputDir)
	res, out := callTool(t, cs, "trader_backtest_result", map[string]any{"run_id": runID})
	require.False(t, res.IsError, toolText(res))
	params := out["report"].(map[string]any)["run"].(map[string]any)["strategy_parameters"].(map[string]any)
	assert.Equal(t, map[string]any{"mode": "external", "strategy_name": "guest", "exec_digest": "sha256:e1", "config_digest": "sha256:c1"}, params)
	assert.NotContains(t, toolText(res), "/home/op")
	assert.NotContains(t, toolText(res), "s3cret")
}
