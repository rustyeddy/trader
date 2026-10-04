package main

import (
	"archive/zip"
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
)

// spyJanuary2020 is a synthetic Stooq daily file for SPY: ten US trading
// days (2 January is the first after the New Year holiday; weekends
// skipped). The prices are made up; nothing here is a real archive.
const spyJanuary2020 = "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n" +
	"SPY.US,D,20200102,000000,100,101,99,100.5,1000,0\n" +
	"SPY.US,D,20200103,000000,100.5,102,100,101,1000,0\n" +
	"SPY.US,D,20200106,000000,101,102,100,101.5,1000,0\n" +
	"SPY.US,D,20200107,000000,101.5,103,101,102,1000,0\n" +
	"SPY.US,D,20200108,000000,102,103,101,102.5,1000,0\n" +
	"SPY.US,D,20200109,000000,102.5,104,102,103,1000,0\n" +
	"SPY.US,D,20200110,000000,103,104,102,103.5,1000,0\n" +
	"SPY.US,D,20200113,000000,103.5,105,103,104,1000,0\n" +
	"SPY.US,D,20200114,000000,104,105,103,104.5,1000,0\n" +
	"SPY.US,D,20200115,000000,104.5,106,104,105,1000,0\n"

// workflowRoots are one isolated Trader installation: every root a
// temporary directory, the stooq archive root holding the synthetic SPY
// archive.
type workflowRoots struct {
	store, raw, archive, runs string
}

func newWorkflowRoots(t *testing.T) workflowRoots {
	t.Helper()
	dir := t.TempDir()
	r := workflowRoots{
		store: filepath.Join(dir, "canonical"), raw: filepath.Join(dir, "raw"),
		archive: filepath.Join(dir, "archive"), runs: filepath.Join(dir, "runs"),
	}
	require.NoError(t, os.MkdirAll(r.archive, 0o755))
	f, err := os.Create(filepath.Join(r.archive, "spy_us_d.zip"))
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create("data/daily/us/nyse etfs/spy.us.txt")
	require.NoError(t, err)
	_, err = w.Write([]byte(spyJanuary2020))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	return r
}

// serve starts trader-mcp's real composition (build) over roots, with
// stooq as the default provider, and returns a connected MCP client.
func (r workflowRoots) serve(t *testing.T, args ...string) *mcp.ClientSession {
	t.Helper()
	env, _ := baseEnv(t) // also pins XDG_DATA_HOME to a temp dir
	set := map[string]string{
		"TRADER_STORE_ROOT": r.store, "TRADER_RAW_ROOT": r.raw, "TRADER_ARCHIVE_ROOT": r.archive,
		"TRADER_BACKTEST_OUTPUT_DIR": r.runs, "TRADER_PROVIDER": "stooq",
	}
	for i, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); set[k] != "" {
			env[i] = k + "=" + set[k]
			delete(set, k)
		}
	}
	for k, v := range set {
		env = append(env, k+"="+v)
	}
	srv, closer, err := build(args, env, &bytes.Buffer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "workflow", Version: "v1"}, nil).Connect(ctx, clientT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call invokes tool and returns its structured result, failing the stage
// (with the tool's error text) unless wantError says the call should fail.
func workflowCall(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, wantError bool) (map[string]any, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	require.NoError(t, err, "%s: protocol error", tool)
	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*mcp.TextContent); ok {
			text = tc.Text
		}
	}
	require.Equal(t, wantError, res.IsError, "%s: unexpected outcome: %s", tool, text)
	if res.IsError {
		return nil, text
	}
	out, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "%s: no structured result", tool)
	return out, text
}

func first(t *testing.T, out map[string]any, key string) map[string]any {
	t.Helper()
	list, ok := out[key].([]any)
	require.True(t, ok && len(list) > 0, "result has no %s", key)
	return list[0].(map[string]any)
}

// TestResearchWorkflow is the MCP Server milestone's closeout test
// (#440): through MCP protocol calls only, against trader-mcp's real
// composition and isolated stores, a client inspects data, sees the write
// gate hold, canonicalizes a dataset, sees coverage advance, runs a
// backtest on that data, and reads the persisted report back. Each stage
// is its own subtest, so a failure names the stage; later stages do not
// run after one fails.
func TestResearchWorkflow(t *testing.T) {
	roots := newWorkflowRoots(t)
	readOnly := roots.serve(t)
	symbols := []string{"SPY"}
	dataset := map[string]any{"symbols": symbols, "interval": "D1"}

	var (
		instrumentID string
		canonical    map[string]any
		runID        string
		configDigest string
		summary      map[string]any
	)
	stage := func(name string, fn func(t *testing.T)) {
		if !t.Run(name, fn) {
			t.Fatalf("research workflow stopped at stage %q", name)
		}
	}

	stage("1 instruments", func(t *testing.T) {
		out, _ := workflowCall(t, readOnly, "trader_instruments", map[string]any{"symbols": symbols}, false)
		assert.Equal(t, "stooq", out["provider"])
		spy := first(t, out, "instruments")
		assert.Equal(t, "etf", spy["kind"])
		assert.Equal(t, "ARCA", spy["exchange"])
		instrumentID, _ = spy["instrument_id"].(string)
		require.NotEmpty(t, instrumentID)
	})

	stage("2 coverage before", func(t *testing.T) {
		out, _ := workflowCall(t, readOnly, "trader_marketdata_coverage", dataset, false)
		spy := first(t, out, "results")
		assert.Equal(t, instrumentID, spy["instrument_id"], "the same instrument as stage 1")
		assert.Empty(t, spy["error"])
		assert.Nil(t, spy["canonical"], "nothing is built yet")
		assert.Nil(t, spy["range"])
		assert.Empty(t, spy["partitions"])
	})

	stage("3 canonicalize refused without writes", func(t *testing.T) {
		_, text := workflowCall(t, readOnly, "trader_marketdata_canonicalize", dataset, true)
		assert.Contains(t, text, "data-mutating tools are disabled", "the documented writes-disabled error")
		assert.Contains(t, text, "--allow-writes")
		entries, err := os.ReadDir(roots.raw)
		assert.True(t, os.IsNotExist(err) || len(entries) == 0, "nothing was imported")
	})

	writable := roots.serve(t, "--allow-writes")

	stage("4 canonicalize with writes", func(t *testing.T) {
		out, _ := workflowCall(t, writable, "trader_marketdata_canonicalize", dataset, false)
		assert.Equal(t, true, out["ok"])
		spy := first(t, out, "results")
		assert.Equal(t, instrumentID, spy["instrument_id"])
		assert.Equal(t, "built", spy["status"])
		assert.Equal(t, "archive", spy["source"], "converted from the synthetic archive")
		assert.Equal(t, map[string]any{"first_date": "2020-01-02", "last_date": "2020-01-15", "rows": float64(10)}, spy["archive"])
		assert.Equal(t, float64(10), spy["published_bars"])
		assert.Nil(t, spy["canonical_before"])
		canonical, _ = spy["canonical_after"].(map[string]any)
		require.NotNil(t, canonical)
	})

	stage("5 coverage after", func(t *testing.T) {
		out, _ := workflowCall(t, readOnly, "trader_marketdata_coverage", dataset, false)
		spy := first(t, out, "results")
		assert.Equal(t, instrumentID, spy["instrument_id"])
		assert.Equal(t, canonical, spy["canonical"], "the canonical span stage 4 reported")
		assert.Equal(t, "2020-01-02T00:00:00Z", canonical["first"])
		assert.Equal(t, "2020-01-15T00:00:00Z", canonical["last"])
		partition := first(t, spy, "partitions")
		assert.Equal(t, map[string]any{"month": "2020-01", "status": "current", "bar_count": float64(10)}, partition)
	})

	stage("6 run backtest", func(t *testing.T) {
		// The read-only server: the run must use the data stage 4 built,
		// since this server may not build any.
		out, _ := workflowCall(t, readOnly, "trader_run_backtest", map[string]any{
			"symbols": symbols, "interval": "D1", "from": "2020-01-02", "to": "2020-01-16", "adverse_distance": "2",
		}, false)
		runID, _ = out["run_id"].(string)
		configDigest, _ = out["config_digest"].(string)
		require.NotEmpty(t, runID)
		require.True(t, strings.HasPrefix(configDigest, "sha256:"), configDigest)
		summary = out["summary"].(map[string]any)
		ds := first(t, summary, "dataset")
		assert.Equal(t, "stooq", ds["provider"])
		assert.Equal(t, instrumentID, ds["instrument"], "the backtest used the dataset stage 4 built")
		assert.Equal(t, "D1", ds["interval"])
		assert.Equal(t, float64(1), summary["open_trade_count"], "the demo strategy entered once")
	})

	stage("7 backtest result", func(t *testing.T) {
		out, _ := workflowCall(t, readOnly, "trader_backtest_result", map[string]any{"run_id": runID}, false)
		assert.Equal(t, runID, out["run_id"])
		report := out["report"].(map[string]any)
		run := report["run"].(map[string]any)
		assert.Equal(t, runID, run["run_id"])
		assert.Equal(t, configDigest, run["config_digest"])
		for _, section := range []string{"run", "dataset", "performance", "trade_stats", "margin", "account"} {
			assert.Equal(t, summary[section], report[section], "the persisted %s section matches the run's summary", section)
		}
		assert.Len(t, report["open_trades"], 1)
		assert.NotEmpty(t, report["equity_curve"])
		assert.FileExists(t, filepath.Join(roots.runs, runID+".json"), "persisted in the configured output directory")
	})
}

// TestResearchWorkflow_ResultIsStable: reading a run back twice returns
// the same report, so the persisted round trip is not a one-shot.
func TestResearchWorkflow_ResultIsStable(t *testing.T) {
	roots := newWorkflowRoots(t)
	cs := roots.serve(t, "--allow-writes")
	dataset := map[string]any{"symbols": []string{"SPY"}, "interval": "D1"}
	workflowCall(t, cs, "trader_marketdata_canonicalize", dataset, false)
	out, _ := workflowCall(t, cs, "trader_run_backtest", map[string]any{
		"symbols": []string{"SPY"}, "interval": "D1", "from": "2020-01-02", "to": "2020-01-16", "adverse_distance": "2",
	}, false)
	args := map[string]any{"run_id": out["run_id"]}
	a, _ := workflowCall(t, cs, "trader_backtest_result", args, false)
	b, _ := workflowCall(t, cs, "trader_backtest_result", args, false)
	aj, err := json.Marshal(a)
	require.NoError(t, err)
	bj, err := json.Marshal(b)
	require.NoError(t, err)
	assert.JSONEq(t, string(aj), string(bj))
}
