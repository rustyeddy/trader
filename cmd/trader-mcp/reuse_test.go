package main

import (
	"context"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrentCallsShareProviderServices is issue #442's acceptance
// test: trader-mcp reuses one market-data service per provider for its
// lifetime, and concurrent calls — to the same provider and to different
// ones, reads and writes, and backtests — stay correct (run under -race).
// Instrument resolution is deterministic across calls, and the shared
// stores end in the state a serial run would leave.
func TestConcurrentCallsShareProviderServices(t *testing.T) {
	roots := newWorkflowRoots(t) // stooq default, synthetic SPY archive
	cs := roots.serve(t, "--allow-writes")
	ctx := context.Background()

	type call struct {
		tool string
		args map[string]any
	}
	reads := []call{
		{"trader_instruments", map[string]any{"symbols": []string{"SPY", "QQQ", "AAPL"}}},
		{"trader_instruments", map[string]any{"symbols": []string{"EURUSD", "USDJPY"}, "provider": "oanda"}},
		{"trader_marketdata_coverage", map[string]any{"symbols": []string{"SPY"}, "interval": "D1"}},
		{"trader_marketdata_coverage", map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1", "provider": "oanda"}},
	}
	canonicalize := call{"trader_marketdata_canonicalize", map[string]any{"symbols": []string{"SPY"}, "interval": "D1"}}
	backtest := call{"trader_run_backtest", map[string]any{
		"symbols": []string{"SPY"}, "interval": "D1", "from": "2020-01-02", "to": "2020-01-16", "adverse_distance": "2",
	}}
	// Phase 1 builds the data concurrently with reads. Phase 2 runs
	// backtests (which read, and prepare, the data) concurrently with
	// more reads and canonicalize calls. A backtest can only run once the
	// archive has been converted at least once, so it waits for phase 1.
	phases := [][]call{
		append(append([]call{}, reads...), canonicalize, canonicalize),
		append(append([]call{}, reads...), backtest, backtest, canonicalize),
	}

	const rounds = 4
	var mu sync.Mutex
	ids := map[string]map[string]bool{} // symbol -> instrument IDs seen
	for _, phase := range phases {
		var wg sync.WaitGroup
		for r := 0; r < rounds; r++ {
			for _, c := range phase {
				wg.Add(1)
				go func() {
					defer wg.Done()
					res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
					if !assert.NoError(t, err, c.tool) {
						return
					}
					if !assert.False(t, res.IsError, "%s: %s", c.tool, toolText(res)) {
						return
					}
					out := res.StructuredContent.(map[string]any)
					switch c.tool {
					case "trader_marketdata_canonicalize":
						// A per-symbol failure is reported in the result,
						// not as a call error.
						assert.Equal(t, true, out["ok"], "canonicalize: %v", out["results"])
						return
					case "trader_marketdata_coverage":
						for _, item := range out["results"].([]any) {
							assert.Empty(t, item.(map[string]any)["error"], "coverage: %v", item)
						}
						return
					case "trader_run_backtest":
						return
					}
					for _, item := range out["instruments"].([]any) {
						inst := item.(map[string]any)
						assert.Empty(t, inst["error"], inst["symbol"])
						mu.Lock()
						sym := inst["symbol"].(string)
						if ids[sym] == nil {
							ids[sym] = map[string]bool{}
						}
						ids[sym][inst["instrument_id"].(string)] = true
						mu.Unlock()
					}
				}()
			}
		}
		wg.Wait()
	}

	for sym, seen := range ids {
		assert.Len(t, seen, 1, "%s resolved to one instrument across every concurrent call", sym)
	}
	require.Len(t, ids, 5)

	// The stores end as a serial run leaves them: SPY's January is built
	// and current, exactly once over.
	out, _ := workflowCall(t, cs, "trader_marketdata_coverage", map[string]any{"symbols": []string{"SPY"}, "interval": "D1"}, false)
	spy := first(t, out, "results")
	assert.Equal(t, map[string]any{"month": "2020-01", "status": "current", "bar_count": float64(10)}, first(t, spy, "partitions"))
}
