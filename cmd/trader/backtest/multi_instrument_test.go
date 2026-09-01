package backtest_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
)

// multiInstrumentReportView is the subset of a JSON-rendered
// report.BacktestReport this file's tests inspect.
type multiInstrumentReportView struct {
	Run struct {
		ConfigDigest string `json:"config_digest"`
	} `json:"run"`
	OpenTrades []struct {
		Instrument string `json:"instrument"`
	} `json:"open_trades"`
	Account struct {
		OpenPositions []struct {
			Instrument string `json:"instrument"`
		} `json:"open_positions"`
	} `json:"account"`
}

func runMultiInstrumentBacktest(t *testing.T, outputDir, dataStoreRoot string, symbols ...string) multiInstrumentReportView {
	t.Helper()

	args := []string{
		"run",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--starting-cash", "10000",
		"--currency", "USD",
		"--risk-fraction", "0.01",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--data-store-root", dataStoreRoot,
		"--output-dir", outputDir,
		"--format", "json",
	}
	for _, s := range symbols {
		args = append(args, "--symbol", s)
	}

	cmd := cmdbacktest.New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())

	var view multiInstrumentReportView
	require.NoError(t, json.Unmarshal(out.Bytes(), &view))
	return view
}

// TestVerticalSlice_MultiInstrumentRun exercises issue #224's own newly
// exposed composition path: repeatable --symbol -> N strategy.
// DataRequirements -> one service/backtest.Run -> one backtest.Runner/
// Scheduler/shared account/pipeline -> one rendered portfolio report.
// This is deliberately not re-proving same-timestamp ordering or
// broad two-instrument determinism — backtest's own scheduler tests
// and #223's determinism suite already do that — it proves the CLI
// wiring itself correctly carries a multi-instrument request through
// to a coherent shared-account result (no per-symbol engine fork:
// both instruments' open positions land in the exact same
// account.open_positions list from one Run call).
func TestVerticalSlice_MultiInstrumentRun(t *testing.T) {
	view := runMultiInstrumentBacktest(t, t.TempDir(), t.TempDir(), "EURUSD", "GBPUSD")

	require.Len(t, view.OpenTrades, 2, "both instruments must have opened a position through the one shared Scheduler/account")
	require.Len(t, view.Account.OpenPositions, 2, "the one shared account must hold both instruments' positions simultaneously")

	gotInstruments := map[string]bool{}
	for _, p := range view.Account.OpenPositions {
		gotInstruments[p.Instrument] = true
	}
	assert.True(t, gotInstruments["fx:EUR/USD"], "expected an open EUR/USD position, got %+v", view.Account.OpenPositions)
	assert.True(t, gotInstruments["fx:GBP/USD"], "expected an open GBP/USD position, got %+v", view.Account.OpenPositions)
}

// TestVerticalSlice_MultiInstrumentRun_ConfigDigestIsOrderIndependent
// proves --symbol flag order never becomes semantically meaningful
// (issue #224 review, point 3): requesting the identical instrument
// set in a different order must produce the identical ConfigDigest.
// RunID is not compared here — environmentFactory seeds each run's
// IDs from id.Random{}, so two separate CLI invocations never share a
// RunID regardless of symbol order, matching ADR-039/#223's own
// execution-identity-versus-configuration-identity distinction.
//
// Both invocations deliberately share one --data-store-root: while
// developing this test, two invocations with *separate* canonical
// stores (each independently calling Manager.Build) produced different
// ConfigDigest values regardless of symbol order, because
// backtest.Manifest.ConfigDigest embeds each dataset's full
// marketdata.Manifest — including BuiltAt, the wall-clock time Manager
// stamped that specific Build call — and marketdata.Manifest carries
// no MarshalJSON of its own that excludes it the way its own
// Revision()/RawFingerprint digest already does. That is a real,
// pre-existing gap in backtest.Manifest's own reproducibility
// guarantee, orthogonal to instrument ordering and to #224's own
// scope — not something to silently patch here. Sharing one store
// means the second invocation's own Plan finds the data already built
// and never re-stamps BuiltAt, isolating this test to what it actually
// intends to prove: order-independence of the requested instrument
// set, not reproducibility of Manager.Build's own timestamp.
func TestVerticalSlice_MultiInstrumentRun_ConfigDigestIsOrderIndependent(t *testing.T) {
	storeRoot := t.TempDir()
	forward := runMultiInstrumentBacktest(t, t.TempDir(), storeRoot, "EURUSD", "GBPUSD")
	reversed := runMultiInstrumentBacktest(t, t.TempDir(), storeRoot, "GBPUSD", "EURUSD")

	require.NotEmpty(t, forward.Run.ConfigDigest)
	assert.Equal(t, forward.Run.ConfigDigest, reversed.Run.ConfigDigest,
		"requesting the same instrument set in a different --symbol order must produce the identical ConfigDigest")
}

// TestVerticalSlice_MultiInstrumentRun_RejectsDuplicateSymbol proves a
// duplicate --symbol is a clear CLI validation error, not something
// that reaches Replay/Runner and fails deeper in the stack (issue
// #224 review, point 3).
func TestVerticalSlice_MultiInstrumentRun_RejectsDuplicateSymbol(t *testing.T) {
	cmd := cmdbacktest.New()
	cmd.SetArgs([]string{
		"run",
		"--symbol", "EURUSD",
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--output-dir", t.TempDir(),
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

// TestVerticalSlice_MultiInstrumentRun_RejectsCaseInsensitiveDuplicate
// proves duplicate detection normalizes case/whitespace before
// comparing, so "--symbol eurusd --symbol EURUSD" is caught too, not
// only a byte-for-byte repeated flag value (issue #224 review, point
// 3's "normalize the CLI vocabulary first").
func TestVerticalSlice_MultiInstrumentRun_RejectsCaseInsensitiveDuplicate(t *testing.T) {
	cmd := cmdbacktest.New()
	cmd.SetArgs([]string{
		"run",
		"--symbol", "eurusd",
		"--symbol", " EURUSD ",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--output-dir", t.TempDir(),
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}
