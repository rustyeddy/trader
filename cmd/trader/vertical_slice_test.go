package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestM25VerticalSlice is issue #112's own primary deliverable: one
// coherent, narrated proof that the complete M2.5 path works through
// every supported boundary --
//
//	trader (Cobra command) -> service/marketdata (application service)
//	   -> marketdata.Manager (public M2 API) -> canonical store/raw archive
//
// -- driven entirely through the real command tree (newRootCmd,
// ExecuteContext), the way an actual operator invocation works, never
// by calling a service or Manager method directly from a test. It
// deliberately does not re-cover every edge case datacmd_test.go/
// datamutation_test.go/format_test.go already exercise in isolation
// (invalid arguments, missing config, JSON structure detail,
// individual cancellation and no-hidden-write checks per command,
// write-error propagation) -- this test's own job is only to prove the
// full chain composes correctly end to end, once, as a single story.
//
// Sync's real network round trip (issue #106/#110's own established
// pattern) is what proves the CLI path reaches all the way through
// Manager's OANDA acquisition, not just its canonical read/build path.
func TestM25VerticalSlice(t *testing.T) {
	body := oandaCandlesJSON([]time.Time{
		time.Date(2024, time.January, 2, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 23, 0, 0, 0, time.UTC),
	})
	server := newFakeOANDAServer(t, body)
	storeRoot := t.TempDir()
	rawRoot := t.TempDir() + "/oanda"
	require.NoError(t, os.MkdirAll(rawRoot, 0o755))
	span := []string{"--from", "2024-01-02T22:00:00Z", "--to", "2024-01-03T00:00:00Z"}

	t.Run("update acquires and publishes from nothing", func(t *testing.T) {
		args := append([]string{"update", "EURUSD", "H1"}, span...)
		out, err := runDataWithOANDA(t, storeRoot, rawRoot, server.URL, args...)
		require.NoError(t, err, "the full Plan -> Sync -> Build orchestration must complete "+
			"through service/marketdata's own Update use case -- this command handler "+
			"never reimplements that composition itself (dataupdate.go's own doc comment)")
		require.Contains(t, out, "downloaded")
		require.Contains(t, out, "published")
	})

	t.Run("bars reads back what update published, in JSON", func(t *testing.T) {
		args := append([]string{"bars", "EURUSD", "H1"}, append(span, "--format", "json")...)
		out, err := runData(t, storeRoot, rawRoot, args...)
		require.NoError(t, err)

		var decoded struct {
			Bars []struct {
				Open string `json:"open"`
			} `json:"bars"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &decoded),
			"JSON output must be a stable, self-contained document -- no internal "+
				"marketdata type leaks into it (issue #112's own acceptance criterion), "+
				"only the plain view types format_json.go defines")
		require.Len(t, decoded.Bars, 2)
		require.Equal(t, "1.1", decoded.Bars[0].Open)
	})

	t.Run("coverage reports the dataset fully covered", func(t *testing.T) {
		args := append([]string{"coverage", "EURUSD", "H1"}, span...)
		out, err := runData(t, storeRoot, rawRoot, args...)
		require.NoError(t, err)
		require.NotContains(t, out, "missing",
			"coverage must not report any gap for a range update just fully published")
	})

	t.Run("plan reports nothing further required", func(t *testing.T) {
		args := append([]string{"plan", "EURUSD", "H1"}, span...)
		out, err := runData(t, storeRoot, rawRoot, args...)
		require.NoError(t, err)
		require.Contains(t, out, "nothing required")
	})

	t.Run("re-running update is a safe, deterministic no-op", func(t *testing.T) {
		// No OANDA credentials configured this time (plain runData, not
		// runDataWithOANDA): proves the re-run genuinely performs no
		// Sync -- consistent with service/marketdata's own
		// TestUpdate_NothingToDoWhenAlreadyCurrent (#107) -- not just
		// that it happens to succeed.
		args := append([]string{"update", "EURUSD", "H1"}, span...)
		out, err := runData(t, storeRoot, rawRoot, args...)
		require.NoError(t, err)
		require.Contains(t, out, "already current")
	})

	// A context already cancelled before ExecuteContext is rejected by
	// data's own PersistentPreRunE (cmd.Context().Err(), checked before
	// building the service) -- the service and *marketdata.Manager are
	// never reached on this path, so this subtest proves only that much,
	// not deeper propagation. That narrower scope is deliberate and
	// honestly named below, per review feedback on an earlier version
	// of this test that claimed more than it exercised. The
	// service-layer and Manager's own ctx handling are each already
	// exercised directly, with a context genuinely reaching that far,
	// by service/marketdata's own TestBars_PropagatesContextCancellation
	// (#105) and marketdata's own per-operation cancellation tests --
	// this test does not need to re-prove that here.
	t.Run("an already-cancelled context is rejected before the pre-run wiring ever calls the service", func(t *testing.T) {
		root, cleanup := newRootCmd()
		defer func() { _ = cleanup() }()
		args := append([]string{"data", "bars", "EURUSD", "H1"}, span...)
		args = append(args, "--store-root", storeRoot, "--raw-root", rawRoot)
		root.SetArgs(args)
		root.SetOut(new(discard))
		root.SetErr(new(discard))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := root.ExecuteContext(ctx)
		require.ErrorIs(t, err, context.Canceled,
			"data's PersistentPreRunE must reject an already-cancelled context "+
				"immediately, before ever constructing the service or touching Manager")
	})
}
