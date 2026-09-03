package backtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// extractRunID pulls report.run.run_id out of a JSON-rendered
// report.BacktestReport, without depending on the report package's
// own (unexported-to-this-test) Go types.
func extractRunID(t *testing.T, jsonOutput string) string {
	t.Helper()
	var doc struct {
		Run struct {
			RunID string `json:"run_id"`
		} `json:"run"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &doc))
	require.NotEmpty(t, doc.Run.RunID)
	return doc.Run.RunID
}

// TestVerticalSlice_RunThenShow exercises the full acceptance
// criterion (issue #222): CLI -> service/backtest -> backtest.Runner
// -> M4 pipeline -> simulator. "run" replays the committed EUR/USD H1
// fixture with the package's own demoStrategy (a single deterministic
// buy entry on the first bar), which must be sized, planned, risk-
// admitted, submitted to the simulated broker, and filled — a
// consequence that can only exist if the canonical M4 pipeline and
// simulator actually ran, not merely that the process exited 0.
// "show" then renders the exact same persisted report with no
// recomputation.
func TestVerticalSlice_RunThenShow(t *testing.T) {
	outputDir := t.TempDir()

	runCmd := cmdbacktest.New()
	var runOut bytes.Buffer
	runCmd.SetOut(&runOut)
	runCmd.SetArgs([]string{
		"run",
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
	assert.Contains(t, runOutput, `"run_id"`)

	// A filled entry is the M4-pipeline/simulator consequence this test
	// exists to prove: either a closed trade or, since demoStrategy
	// never exits, an open one — either way, at least one position must
	// have actually been submitted and filled, not merely proposed.
	assert.True(t,
		bytes.Contains([]byte(runOutput), []byte(`"open_trades"`)) &&
			!bytes.Contains([]byte(runOutput), []byte(`"open_trades": []`)),
		"expected at least one open trade in run output, proving the M4 pipeline and simulator actually filled an order:\n%s", runOutput)

	runID := extractRunID(t, runOutput)

	showCmd := cmdbacktest.New()
	var showOut bytes.Buffer
	showCmd.SetOut(&showOut)
	showCmd.SetArgs([]string{
		"show", runID,
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, showCmd.Execute())

	// A raw string comparison, not assert.JSONEq: JSONEq only checks
	// semantic equality (whitespace/field-order differences would still
	// pass), but the point of this assertion is that show performs
	// literally zero rendering-affecting recomputation — it must
	// produce byte-for-byte the same encoding run did (issue #240
	// review).
	assert.Equal(t, runOutput, showOut.String(), "show must render byte-for-byte the same output run produced, with no recomputation")
}

// TestVerticalSlice_RunWithWarmupBars_EntersAfterWarmupAndFillsAtCorrectBar
// is the PR #240 second-review regression: Scheduler calls Strategy.
// OnBar during every one of its declared WarmupBars warm-up bars too,
// discarding whatever intents it returns rather than suppressing the
// call itself — an earlier version of demoStrategy set its own
// "entered" flag on its very first OnBar callback, so with
// --warmup-bars > 0 it spent its one entry on a warm-up bar whose
// intent Scheduler silently discarded, and the run showed zero trades.
// This test uses the deliberately gapped February fixture
// (run_test.go's own newGappedFixtureManager fixture data) with
// --warmup-bars 1: the strategy's first *honored* OnBar is bar index
// 1 (2024-02-01T01:00), so its fill must land on bar index 2
// (2024-02-01T02:00) at that bar's own Open — read independently here
// via *marketdata.Manager, the same way run_test.go's unit tests
// establish ground truth, so this assertion cannot pass merely because
// a wrong bar's price happens to coincide.
func TestVerticalSlice_RunWithWarmupBars_EntersAfterWarmupAndFillsAtCorrectBar(t *testing.T) {
	outputDir := t.TempDir()

	runCmd := cmdbacktest.New()
	var runOut bytes.Buffer
	runCmd.SetOut(&runOut)
	runCmd.SetArgs([]string{
		"run",
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-02-01T00:00:00Z",
		"--to", "2024-02-01T03:00:00Z",
		"--starting-cash", "10000",
		"--currency", "USD",
		"--risk-fraction", "0.01",
		"--adverse-distance", "0.01000",
		"--warmup-bars", "1",
		"--data-raw-root", "testdata/raw/oanda",
		"--data-store-root", t.TempDir(),
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, runCmd.Execute())

	var doc struct {
		OpenTrades []struct {
			OpenedAt time.Time `json:"opened_at"`
		} `json:"open_trades"`
		Account struct {
			OpenPositions []struct {
				AvgPrice string `json:"avg_price"`
			} `json:"open_positions"`
		} `json:"account"`
	}
	require.NoError(t, json.Unmarshal(runOut.Bytes(), &doc))
	require.Len(t, doc.OpenTrades, 1, "the demo strategy must still trade once warm-up has cleared:\n%s", runOut.String())
	require.Len(t, doc.Account.OpenPositions, 1)

	expectedFillBar := fillBarFor(t, "testdata/raw/oanda", "2024-02-01T02:00:00Z")

	assert.True(t, doc.OpenTrades[0].OpenedAt.Equal(expectedFillBar.Time),
		"expected the fill to be timestamped at bar index 2 (%s), got %s", expectedFillBar.Time, doc.OpenTrades[0].OpenedAt)
	assert.Equal(t, expectedFillBar.Open.String(), doc.Account.OpenPositions[0].AvgPrice,
		"expected the fill price to be bar index 2's own Open (%s)", expectedFillBar.Open)
}

// fillBarFor reads back the canonical H1 bar at exactly barTime from
// rawRoot's own EUR/USD fixture, independent of anything run.go
// itself computes, so tests asserting against it cannot pass merely by
// agreeing with a shared bug.
func fillBarFor(t *testing.T, rawRoot, barTime string) marketdata.Bar {
	t.Helper()

	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurusd,
		Provider:   "oanda",
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)

	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(listing))

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	target, err := time.Parse(time.RFC3339, barTime)
	require.NoError(t, err)
	span, err := marketdata.NewTimeRange(target, target.Add(time.Hour))
	require.NoError(t, err)

	ctx := context.Background()
	plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: listing.InstrumentID(), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	reader, err := manager.Bars(ctx, marketdata.BarQuery{Instrument: listing.InstrumentID(), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	bar, err := reader.Next(ctx)
	require.NoError(t, err)
	return bar
}

// TestVerticalSlice_ShowRejectsUnknownRunID proves show fails clearly
// rather than fabricating output for a run that was never persisted.
func TestVerticalSlice_ShowRejectsUnknownRunID(t *testing.T) {
	outputDir := t.TempDir()

	showCmd := cmdbacktest.New()
	showCmd.SetArgs([]string{
		"show", "run_01HK153X003DMSTAVHVKTAA8HV",
		"--output-dir", outputDir,
	})
	require.Error(t, showCmd.Execute())
}
