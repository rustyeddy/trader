// Package m2_test is an external consumer of Trader's M2 public API: it
// imports only public packages (clock, instrument, marketdata, num), the
// way real code outside this module would. It exists for issue #82
// (M2-14): proving the M2 historical-data subsystem is one coherent,
// externally consumable whole — acquire, normalize, validate, store,
// query, resample, and report coverage for one real data path — driven
// entirely through marketdata.Manager's public methods (Plan, Build,
// Bars, Coverage), never by reaching into a provider, store, or builder
// directly. No internal marketdata type appears anywhere in this file,
// by construction: this package cannot import marketdata/internal/...
// at all.
//
// testdata/raw holds a small, hand-sized, committed raw OANDA archive
// (two trading weeks of EUR/USD H1 and D1 data, deliberately missing one
// day) — no network access or private archive is required to run this
// package. See corpus_test.go (behind the corpus build tag) for the
// optional full-archive counterpart.
package m2_test

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureRawRoot is this package's own committed raw archive, in
// ADR-020's root/PAIR/YYYY/MM/PAIR-YYYY-MM-tf.csv layout: EUR/USD H1 and
// D1 for two trading weeks starting 2024-01-07 (a Sunday reopen), with
// 2024-01-16 (a Tuesday) deliberately absent from both files — the
// vertical slice's own missing-data fixture, not a mistake.
const fixtureRawRoot = "testdata/raw/oanda"

// eurusdListing builds the EUR/USD instrument this fixture data is keyed
// under, through the same public instrument constructors a real
// composition root would use.
func eurusdListing(t *testing.T) instrument.Listing {
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
	return listing
}

// copyFixtureRaw copies fixtureRawRoot into a fresh, writable temp
// directory and returns it. Every test in this package operates on its
// own copy, never the checked-in fixture itself — the "mutate the raw
// file to prove staleness detection" scenario would otherwise corrupt
// the committed fixture for every other test and every future run.
func copyFixtureRaw(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(fixtureRawRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(fixtureRawRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	require.NoError(t, err)
	return dst
}

// newTestManager builds a Manager wired to a fresh, writable copy of
// this package's committed raw fixture and a fresh canonical StoreRoot,
// so every test starts from a clean, isolated slate. It returns the raw
// and store roots alongside the Manager for tests that need to inspect
// or mutate the filesystem directly (staleness, no-hidden-writes):
// Manager itself never exposes either path through any public
// operation, by design (see Config's own RawRoot/StoreRoot doc
// comments), so a test that needs them must keep its own copy from
// construction time rather than ask Manager for them back.
func newTestManager(t *testing.T) (mgr *marketdata.Manager, rawRoot, storeRoot string) {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t)))

	rawRoot = copyFixtureRaw(t)
	storeRoot = t.TempDir()
	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    storeRoot,
		RawRoot:      rawRoot,
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	return mgr, rawRoot, storeRoot
}

func eurusdID(t *testing.T) instrument.ID {
	t.Helper()
	return eurusdListing(t).InstrumentID()
}

// planAndBuild runs Plan then Build for interval over span, returning
// Build's result. It is the vertical slice's own repeated step: every
// canonical dataset in this package is produced this way, never by
// calling an internal builder directly.
func planAndBuild(t *testing.T, mgr *marketdata.Manager, interval marketdata.Interval, span marketdata.TimeRange) marketdata.BuildResult {
	t.Helper()
	ctx := context.Background()
	plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: interval, Range: span})
	require.NoError(t, err)
	result, err := mgr.Build(ctx, plan)
	require.NoError(t, err)
	return result
}

// fixtureSpan is the exact half-open range this package's raw fixture
// covers: two trading weeks, 2024-01-07 (Sunday reopen) through
// 2024-01-19 22:00 UTC (the close of the second week's last session).
// It deliberately stops exactly at the fixture's own last record,
// rather than extending into any later, unpopulated date: a query range
// reaching past raw's last record makes Plan schedule an ActionDownloadRaw
// "extend" (needsExtend, plan.go) alongside whatever else it schedules,
// which every step below except the deliberate stale-rebuild one assumes
// does not happen.
func fixtureSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 19, 22, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// week1Span is the fixture's first, fully complete W1 week
// (2024-01-07 through 2024-01-14): the only range deriveActionsW1 will
// actually schedule a derive for, since its own D1-completeness check
// is clipped to the queried range intersected with the month, and the
// full fixtureSpan's own D1 data has a gap (the deliberate missing
// 2024-01-16) that would otherwise block derivation for the whole
// range, not just the second week that actually needs it.
func week1Span(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 14, 22, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// TestM2VerticalSlice drives the complete M2 historical-data flow —
// acquire (already on disk, this package's own fixture), normalize and
// validate, canonical CSV + manifest publication, Manager query and
// cache, W1 materialization, and coverage/quality reporting — entirely
// through Manager's public API, matching #82's own required scenario.
func TestM2VerticalSlice(t *testing.T) {
	mgr, rawRoot, _ := newTestManager(t)
	ctx := context.Background()
	span := fixtureSpan(t)

	var h1Manifest, d1Manifest, w1Manifest marketdata.Manifest

	t.Run("normalize H1 from raw", func(t *testing.T) {
		result := planAndBuild(t, mgr, marketdata.H1, span)
		require.Len(t, result.Published, 1)
		h1Manifest = result.Published[0].Manifest
		assert.Equal(t, 216, h1Manifest.BarCount, "9 open days x 24 H1 bars, per this package's own fixture")
	})

	t.Run("normalize D1 from raw", func(t *testing.T) {
		result := planAndBuild(t, mgr, marketdata.D1, span)
		require.Len(t, result.Published, 1)
		d1Manifest = result.Published[0].Manifest
		assert.Equal(t, 9, d1Manifest.BarCount, "10 open days minus the deliberately missing 2024-01-16")
	})

	t.Run("derive W1 from D1", func(t *testing.T) {
		// Queried over exactly the first (fully complete) week: Plan's
		// own D1-completeness check is clipped to the queried range, and
		// the fixture's full span has a gap (the deliberate missing
		// 2024-01-16) that would otherwise block scheduling a derive at
		// all — see week1Span's own doc comment.
		plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.W1, Range: week1Span(t)})
		require.NoError(t, err)
		require.Len(t, plan.Actions, 1)
		assert.Equal(t, marketdata.ActionDeriveCanonical, plan.Actions[0].Kind)

		result, err := mgr.Build(ctx, plan)
		require.NoError(t, err)
		require.Len(t, result.Published, 1)
		w1Manifest = result.Published[0].Manifest

		require.NotNil(t, w1Manifest.Parent, "W1 is derived from D1, never built directly from raw")
		assert.Equal(t, marketdata.D1, w1Manifest.Parent.Interval)
		assert.Equal(t, d1Manifest.Revision(), w1Manifest.Parent.Revision,
			"lineage names the exact D1 revision W1 was built from")
		assert.Equal(t, 1, w1Manifest.BarCount,
			"only the first week (2024-01-07 to 01-11) is complete; the second has the deliberate gap")
	})

	t.Run("query the published W1 bar", func(t *testing.T) {
		weekSpan, err := marketdata.NewTimeRange(
			time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 7, 23, 0, 0, 0, time.UTC),
		)
		require.NoError(t, err)
		reader, err := mgr.Bars(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.W1, Range: weekSpan})
		require.NoError(t, err)
		defer reader.Close()

		bar, err := reader.Next(ctx)
		require.NoError(t, err)
		assert.Equal(t, "1.1", bar.Open.String(), "the week's own first D1 bar's Open")
		_, err = reader.Next(ctx)
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("manifest revision is deterministic", func(t *testing.T) {
		// A second, entirely independent Manager (its own store, its own
		// copy of the identical raw fixture) building the same H1
		// dataset must reproduce the exact same Manifest.Revision():
		// canonical dataset identity is reproducible from its inputs,
		// not path-, process-, or history-dependent. Re-Plan/Build on
		// the *same* Manager would not exercise this: once a partition
		// is Current, Plan correctly schedules nothing further to do.
		other, _, _ := newTestManager(t)
		result := planAndBuild(t, other, marketdata.H1, span)
		require.Len(t, result.Published, 1)
		assert.Equal(t, h1Manifest.Revision(), result.Published[0].Manifest.Revision())
	})

	t.Run("coverage reports the deliberate gap and the closed weekend, never conflating them", func(t *testing.T) {
		cov, err := mgr.Coverage(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.D1, Range: span})
		require.NoError(t, err)

		var foundMissing bool
		for _, g := range cov.Gaps {
			assert.NotEqual(t, marketdata.IntervalStateClosed, g.State,
				"a routinely closed day must never appear as a reported Gap")
			if g.State == marketdata.IntervalStateMissing {
				foundMissing = true
				assert.True(t, g.Span.Start().Equal(time.Date(2024, time.January, 16, 22, 0, 0, 0, time.UTC)),
					"the gap is exactly the deliberately omitted 2024-01-16 session")
			}
		}
		assert.True(t, foundMissing, "the deliberate gap must be reported, not silently absorbed")
	})

	t.Run("rebuilding stale raw data changes the published revision", func(t *testing.T) {
		// Mutate this Manager's own raw copy (never the checked-in
		// fixture — see copyFixtureRaw) by editing one existing row's
		// price in place, changing the raw file's content fingerprint
		// without changing its row count or introducing a duplicate
		// timestamp, then confirm Plan detects the resulting mismatch
		// and Build republishes a new revision from the same bar count.
		h1Path := filepath.Join(rawRoot, "EURUSD", "2024", "01", "EURUSD-2024-01-h1.csv")
		original, err := os.ReadFile(h1Path)
		require.NoError(t, err)
		const oldRow = "2024-01-08T12:00:00Z,1.10210,1.10250,1.10180,1.10225"
		const newRow = "2024-01-08T12:00:00Z,1.10210,1.10250,1.10175,1.10225"
		require.Contains(t, string(original), oldRow, "fixture assumption: this exact row exists unmodified")
		mutated := []byte(strings.Replace(string(original), oldRow, newRow, 1))
		require.NoError(t, os.WriteFile(h1Path, mutated, 0o644))

		plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.H1, Range: span})
		require.NoError(t, err)
		require.Len(t, plan.Actions, 1)
		assert.Contains(t, plan.Actions[0].Reason, "stale")

		result, err := mgr.Build(ctx, plan)
		require.NoError(t, err)
		require.Len(t, result.Published, 1)
		assert.NotEqual(t, h1Manifest.Revision(), result.Published[0].Manifest.Revision())
		assert.Equal(t, h1Manifest.BarCount, result.Published[0].Manifest.BarCount,
			"the row count is unchanged; only its content differs")
	})
}

// TestM2Build_CancelledContextPublishesNothing confirms Build leaves no
// partially published canonical dataset when its context is cancelled
// mid-run: a follow-up Coverage call must show no trace of the aborted
// action, not a partial or corrupt one.
func TestM2Build_CancelledContextPublishesNothing(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	span := fixtureSpan(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan, err := mgr.Plan(context.Background(), marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Actions)

	_, err = mgr.Build(ctx, plan)
	assert.ErrorIs(t, err, context.Canceled)

	cov, err := mgr.Coverage(context.Background(), marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	for _, pc := range cov.Partitions {
		assert.Equal(t, marketdata.PartitionCoverageMissing, pc.Status,
			"a cancelled Build must leave every partition exactly as unbuilt as before it ran")
	}
}

// TestM2Queries_NeverWriteToStore confirms Bars and Coverage — read-only
// queries — never touch StoreRoot on disk, even when nothing has been
// published yet: no hidden writes or downloads triggered by a query
// alone.
func TestM2Queries_NeverWriteToStore(t *testing.T) {
	mgr, _, storeRoot := newTestManager(t)
	span := fixtureSpan(t)
	ctx := context.Background()

	_, err := mgr.Coverage(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	_, err = mgr.Bars(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.H1, Range: span})
	assert.Error(t, err, "nothing has been built yet, so Bars must report unavailable data rather than build it on the fly")

	entries, err := os.ReadDir(storeRoot)
	require.NoError(t, err)
	assert.Empty(t, entries, "Coverage and a failed Bars query must never create anything under StoreRoot")
}
