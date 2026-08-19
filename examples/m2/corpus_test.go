//go:build corpus

package m2_test

// This file is excluded from normal `go test ./...` / `make check` by
// the corpus build tag, matching #75's fullArchiveRoot and #81's
// corpusRawRoot precedent exactly: running the full M2 vertical slice
// against a real, multi-decade archive is an operator action against
// real data on disk, not a CI assertion (#82's own acceptance
// criterion: "Optional full-corpus verification is documented"). An
// operator edits corpusRawRoot below locally and runs, for example:
//
//	go test -tags corpus ./examples/m2/... -run TestM2VerticalSlice_Corpus -v
//
// The edit is never committed; the test skips itself whenever
// corpusRawRoot is empty.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/require"
)

// corpusRawRoot is the preserved raw OANDA archive
// (root/PAIR/YYYY/MM/...), the same real-data location #81's own
// corpus tooling points at. Not shipped here, since it is a local
// filesystem path, not a fact about the codebase.
const corpusRawRoot = ""

// TestM2VerticalSlice_Corpus runs the identical Plan/Build/Bars/Coverage
// flow TestM2VerticalSlice exercises against this package's small
// committed fixture, but against one real month from the operator's
// full archive — proving the vertical slice holds at real scale, not
// only against a hand-sized fixture.
func TestM2VerticalSlice_Corpus(t *testing.T) {
	if corpusRawRoot == "" {
		t.Skip("corpusRawRoot is empty; edit the constant in this file to point at a real raw OANDA archive to run this test")
	}
	if info, err := os.Stat(corpusRawRoot); err != nil || !info.IsDir() {
		t.Skipf("corpusRawRoot %q is not a readable directory: %v", corpusRawRoot, err)
	}

	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t)))

	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      corpusRawRoot,
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	ctx := context.Background()
	eurusd := eurusdListing(t).InstrumentID()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	for _, interval := range []marketdata.Interval{marketdata.H1, marketdata.D1} {
		plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: eurusd, Interval: interval, Range: span})
		require.NoError(t, err, "Plan(%s)", interval)
		result, err := mgr.Build(ctx, plan)
		require.NoError(t, err, "Build(%s)", interval)
		t.Logf("%s: published %d partition(s), skipped %d action(s)", interval, len(result.Published), len(result.Skipped))
	}

	plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: eurusd, Interval: marketdata.W1, Range: span})
	require.NoError(t, err, "Plan(W1)")
	result, err := mgr.Build(ctx, plan)
	require.NoError(t, err, "Build(W1)")
	t.Logf("W1: published %d partition(s)", len(result.Published))

	cov, err := mgr.Coverage(ctx, marketdata.BarQuery{Instrument: eurusd, Interval: marketdata.D1, Range: span})
	require.NoError(t, err, "Coverage(D1)")
	t.Logf("D1 coverage: %d partition(s), %d gap(s)", len(cov.Partitions), len(cov.Gaps))

	reader, err := mgr.Bars(ctx, marketdata.BarQuery{Instrument: eurusd, Interval: marketdata.D1, Range: span})
	require.NoError(t, err, "Bars(D1)")
	defer reader.Close()
	var n int
	for {
		_, err := reader.Next(ctx)
		if err != nil {
			break
		}
		n++
	}
	t.Logf("D1 bars read: %d", n)
}
