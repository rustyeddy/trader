package marketdata

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eurusdListing returns a tradable EUR/USD Listing under provider
// "oanda", symbol "EURUSD" — matching validPartitionKey's provider and
// symbol — for tests to register with a MemoryResolver.
func eurusdListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "oanda",
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// testResolver returns a MemoryResolver with eurusdListing registered.
func testResolver(t *testing.T) instrument.Resolver {
	t.Helper()
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(eurusdListing(t)))
	return r
}

// newTestManager returns a Manager rooted at t.TempDir(), wired with
// testResolver and provider "oanda".
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		Resolver:     testResolver(t),
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	return m
}

// publishTestPartition publishes m/bs under key directly through the
// Manager's own store, bypassing Bars — the same shortcut the store's own
// tests use, since Manager has no publish operation yet (issue #78's
// scope is read-only).
func publishTestPartition(t *testing.T, mgr *Manager, key partitionKey, m Manifest, bs BarSet) {
	t.Helper()
	require.NoError(t, mgr.store.publish(context.Background(), key, m, bs))
}

func TestManagerBars_RoundTrip(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	publishTestPartition(t, mgr, key, m, bs)

	reader, err := mgr.Bars(context.Background(), BarQuery{
		Instrument: eurusd(),
		Interval:   H1,
		Range:      m.Span,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	var got []Bar
	for {
		b, err := reader.Next(context.Background())
		if errors.Is(err, errBarReaderClosed) {
			t.Fatal("reader unexpectedly reported closed")
		}
		if err != nil {
			break
		}
		got = append(got, b)
	}
	require.Len(t, got, len(bs.Bars))
	for i := range bs.Bars {
		assert.Equal(t, bs.Bars[i], got[i])
	}

	manifests := reader.Manifests()
	require.Len(t, manifests, 1)
	assert.Equal(t, m.Revision(), manifests[0].Revision())
}

func TestManagerBars_ReturnsIOEOFAtEnd(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	publishTestPartition(t, mgr, key, m, bs)

	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	require.NoError(t, err)

	for i := 0; i < len(bs.Bars); i++ {
		_, err := reader.Next(context.Background())
		require.NoError(t, err)
	}
	_, err = reader.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)
}

func TestManagerBars_NarrowerRangeFiltersBars(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	publishTestPartition(t, mgr, key, m, bs)

	// Request only the first bar's hour.
	narrow, err := NewTimeRange(m.Span.Start(), m.Span.Start().Add(time.Hour))
	require.NoError(t, err)

	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: narrow})
	require.NoError(t, err)

	b, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, bs.Bars[0].Time, b.Time)

	_, err = reader.Next(context.Background())
	assert.Error(t, err) // io.EOF: second bar falls outside narrow range
}

func TestManagerBars_SpansMultipleMonths(t *testing.T) {
	mgr := newTestManager(t)

	aprilStart := time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC)
	aprilEnd := time.Date(2020, 4, 1, 2, 0, 0, 0, time.UTC)

	// March's manifest Span is extended all the way to aprilStart, so its
	// coverage is contiguous with April's: only two actual bars sit in
	// that span (an unrealistically large gap between real observations,
	// but Manifest.Span is a coverage claim, not a promise of bar
	// density — see BarSet's doc comment), yet the *coverage* union has
	// no hole, which is the property Bars actually verifies.
	marchKey := validPartitionKey(t)
	marchManifest := validManifest(t)
	marchBars := validBarSet(t)
	marchSpan, err := NewTimeRange(marchManifest.Span.Start(), aprilStart)
	require.NoError(t, err)
	marchManifest.Span = marchSpan
	marchBars.Span = marchSpan
	publishTestPartition(t, mgr, marchKey, marchManifest, marchBars)

	aprilSpan, err := NewTimeRange(aprilStart, aprilEnd)
	require.NoError(t, err)
	aprilBars := marchBars
	aprilBars.Span = aprilSpan
	aprilBars.Bars = []Bar{barAt(t, aprilStart)}
	aprilManifest := marchManifest
	aprilManifest.Span = aprilSpan
	aprilManifest.BarCount = 1
	aprilManifest.FirstBar = aprilStart
	aprilManifest.LastBar = aprilStart
	aprilKey := marchKey
	aprilKey.year = 2020
	aprilKey.month = time.April
	publishTestPartition(t, mgr, aprilKey, aprilManifest, aprilBars)

	full, err := NewTimeRange(marchManifest.Span.Start(), aprilEnd)
	require.NoError(t, err)
	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: full})
	require.NoError(t, err)

	var got []Bar
	for {
		b, err := reader.Next(context.Background())
		if err != nil {
			break
		}
		got = append(got, b)
	}
	assert.Len(t, got, len(marchBars.Bars)+1)
	assert.Len(t, reader.Manifests(), 2)
}

// TestManagerBars_GapInCoverageReportsErrDataUnavailable is the case a
// design review flagged as silently succeeding in an earlier draft: a
// query range that a partition's own Manifest.Span does not actually
// cover must fail explicitly, even though a partition file exists for
// the touched month.
func TestManagerBars_GapInCoverageReportsErrDataUnavailable(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	publishTestPartition(t, mgr, key, m, validBarSet(t))

	// m.Span covers only March 2 00:00-04:00; ask for a range that
	// extends well past it, still inside the same UTC month so only one
	// (existing) partition is even touched.
	wide, err := NewTimeRange(m.Span.Start(), m.Span.End().Add(20*time.Hour))
	require.NoError(t, err)

	_, err = mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: wide})
	assert.ErrorIs(t, err, ErrDataUnavailable)
}

// TestManagerBars_BoundarySpilloverIsFoundViaProbe reproduces the
// D1/W1 partition-routing gap a design review identified: the canonical
// store's own overlap-not-containment rule (checkKeyMatchesManifest)
// permits a partition filed under one calendar month to hold bars whose
// observed Time actually falls in the *adjacent* month. A query entirely
// within that adjacent month, with no partition file of its own, must
// still find those bars — exactly once — by probing the neighboring key.
func TestManagerBars_BoundarySpilloverIsFoundViaProbe(t *testing.T) {
	mgr := newTestManager(t)

	// Filed under April, but its bars' observed Times are the last two
	// days of March, and its Span only barely pokes into April — the
	// exact shape checkKeyMatchesManifest allows for a session-aligned
	// D1 bar (see store_csv.go), here used with D1 to match that real
	// scenario directly.
	spanStart := time.Date(2020, 3, 30, 17, 0, 0, 0, time.UTC)
	spanEnd := time.Date(2020, 4, 1, 17, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(spanStart, spanEnd)
	require.NoError(t, err)
	bar1 := barAt(t, spanStart)
	bar2 := barAt(t, time.Date(2020, 3, 31, 17, 0, 0, 0, time.UTC))
	bs := BarSet{Instrument: eurusd(), Interval: D1, Span: span, Basis: BasisBid, Bars: []Bar{bar1, bar2}}
	require.NoError(t, bs.Validate())

	man := validManifest(t)
	man.Interval = D1
	man.Span = span
	man.BarCount = 2
	man.FirstBar = bar1.Time
	man.LastBar = bar2.Time
	require.NoError(t, man.Matches(bs))

	aprilKey := partitionKey{
		provider:   "oanda",
		symbol:     "EURUSD",
		instrument: eurusd(),
		interval:   D1,
		year:       2020,
		month:      time.April,
	}
	publishTestPartition(t, mgr, aprilKey, man, bs)

	// Entirely within March; no March partition was ever published. The
	// range starts exactly at spanStart so the April-filed manifest's own
	// Span fully covers it — this test is about partition *routing*, not
	// the separate coverage-gap check.
	queryRange, err := NewTimeRange(spanStart, time.Date(2020, 3, 31, 18, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: D1, Range: queryRange})
	require.NoError(t, err)

	var got []Bar
	for {
		b, err := reader.Next(context.Background())
		if err != nil {
			break
		}
		got = append(got, b)
	}
	require.Len(t, got, 2, "both March-dated bars must be found via the April-filed partition, exactly once each")
	assert.Equal(t, bar1.Time, got[0].Time)
	assert.Equal(t, bar2.Time, got[1].Time)
}

func TestManagerBars_MissingPartitionReportsErrDataUnavailable(t *testing.T) {
	mgr := newTestManager(t)
	span, err := NewTimeRange(
		time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 2, 4, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	_, err = mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, ErrDataUnavailable)
}

func TestManagerBars_InvalidQuery(t *testing.T) {
	mgr := newTestManager(t)
	span, err := NewTimeRange(
		time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 2, 4, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	cases := map[string]BarQuery{
		"ZeroInstrument":  {Instrument: instrument.ID{}, Interval: H1, Range: span},
		"InvalidInterval": {Instrument: eurusd(), Interval: Interval{}, Range: span},
		"ZeroRange":       {Instrument: eurusd(), Interval: H1, Range: TimeRange{}},
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := mgr.Bars(context.Background(), q)
			assert.ErrorIs(t, err, ErrInvalidQuery)
		})
	}
}

func TestManagerBars_UnresolvedInstrumentErrors(t *testing.T) {
	mgr := newTestManager(t)
	span, err := NewTimeRange(
		time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 2, 4, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	_, err = mgr.Bars(context.Background(), BarQuery{Instrument: gbpusd(), Interval: H1, Range: span})
	assert.Error(t, err)
	assert.ErrorIs(t, err, instrument.ErrUnknownSymbol)
}

func TestManagerBars_CancelledContext(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	publishTestPartition(t, mgr, key, validManifest(t), validBarSet(t))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.Bars(ctx, BarQuery{Instrument: eurusd(), Interval: H1, Range: validManifest(t).Span})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestManagerBars_NotConfiguredReportsError(t *testing.T) {
	var mgr Manager
	_, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: validManifest(t).Span})
	assert.ErrorIs(t, err, ErrInvalidConfig)

	var nilMgr *Manager
	_, err = nilMgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: validManifest(t).Span})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestManagerBars_ServedFromCacheAfterFileRemoved(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	publishTestPartition(t, mgr, key, m, bs)

	// Prime the cache.
	_, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	require.NoError(t, err)
	assert.Equal(t, 1, mgr.cache.len())

	path, err := key.path(mgr.storeRoot)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	// Second query must still succeed: it should be served from cache,
	// not re-read the now-missing file.
	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	require.NoError(t, err)
	assert.Len(t, reader.Manifests(), 1)
}

func TestManagerBars_InvalidateForcesReload(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	publishTestPartition(t, mgr, key, m, bs)

	_, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	require.NoError(t, err)
	require.Equal(t, 1, mgr.cache.len())

	mgr.cache.invalidate(key)
	assert.Equal(t, 0, mgr.cache.len())

	path, err := key.path(mgr.storeRoot)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	_, err = mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	assert.ErrorIs(t, err, ErrDataUnavailable, "cache was invalidated, so this must re-read the (now-missing) file")
}

// TestManagerBars_ConcurrentQueries is the -race regression a design
// review asked for at the Manager.Bars level, not just barCache's own
// unit tests: many goroutines querying the same and different partitions
// concurrently through the real Manager.Bars entry point.
func TestManagerBars_ConcurrentQueries(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	publishTestPartition(t, mgr, key, m, validBarSet(t))

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Go(func() {
			reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
			if err != nil {
				errs <- err
				return
			}
			for {
				if _, err := reader.Next(context.Background()); err != nil {
					break
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("unexpected error from concurrent Bars call: %v", err)
	}
}

// TestManagerBars_ManifestMutationDoesNotPoisonSubsequentQuery is the
// end-to-end version of the cache-level aliasing tests: mutating a
// Manifest returned from one query's BarReader.Manifests must not affect
// what a later query, hitting the same cached partition, returns.
func TestManagerBars_ManifestMutationDoesNotPoisonSubsequentQuery(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validDerivedManifest(t)
	publishTestPartition(t, mgr, key, m, validBarSet(t))

	reader1, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	require.NoError(t, err)
	manifests1 := reader1.Manifests()
	require.Len(t, manifests1, 1)
	require.NotNil(t, manifests1[0].Parent)
	manifests1[0].Parent.Revision = "mutated-by-caller"

	reader2, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	require.NoError(t, err)
	manifests2 := reader2.Manifests()
	require.Len(t, manifests2, 1)
	assert.Equal(t, m.Revision(), manifests2[0].Revision())
	assert.Equal(t, m.Parent.Revision, manifests2[0].Parent.Revision)
}

func TestBarQueryValidate(t *testing.T) {
	span, err := NewTimeRange(time.Now(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	valid := BarQuery{Instrument: eurusd(), Interval: H1, Range: span}
	assert.NoError(t, valid.validate())
}

func TestMonthPartitionKeys_SingleMonth(t *testing.T) {
	span, err := NewTimeRange(
		time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 2, 4, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	keys := monthPartitionKeys("oanda", "EURUSD", eurusd(), H1, span)
	require.Len(t, keys, 1)
	assert.Equal(t, 2020, keys[0].year)
	assert.Equal(t, time.March, keys[0].month)
}

func TestMonthPartitionKeys_CrossesMonthBoundary(t *testing.T) {
	span, err := NewTimeRange(
		time.Date(2020, 3, 31, 23, 0, 0, 0, time.UTC),
		time.Date(2020, 4, 1, 1, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	keys := monthPartitionKeys("oanda", "EURUSD", eurusd(), H1, span)
	require.Len(t, keys, 2)
	assert.Equal(t, time.March, keys[0].month)
	assert.Equal(t, time.April, keys[1].month)
}

func TestBoundaryProbeKeys_EmptyCoreReturnsNil(t *testing.T) {
	assert.Nil(t, boundaryProbeKeys(nil))
}

func TestBoundaryProbeKeys_SurroundsCoreRange(t *testing.T) {
	core := []partitionKey{
		{year: 2020, month: time.March},
		{year: 2020, month: time.April},
	}
	probes := boundaryProbeKeys(core)
	require.Len(t, probes, 2)
	assert.Equal(t, 2020, probes[0].year)
	assert.Equal(t, time.February, probes[0].month)
	assert.Equal(t, 2020, probes[1].year)
	assert.Equal(t, time.May, probes[1].month)
}

func TestShiftPartitionKeyMonth_RollsOverYearBoundary(t *testing.T) {
	dec := partitionKey{year: 2020, month: time.December}
	assert.Equal(t, partitionKey{year: 2021, month: time.January}, shiftPartitionKeyMonth(dec, 1))

	jan := partitionKey{year: 2020, month: time.January}
	assert.Equal(t, partitionKey{year: 2019, month: time.December}, shiftPartitionKeyMonth(jan, -1))
}

func TestCoverageGap_FullyCovered(t *testing.T) {
	want, err := NewTimeRange(
		time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	gap, ok := coverageGap(want, []TimeRange{want})
	assert.True(t, ok)
	assert.Equal(t, TimeRange{}, gap)
}

func TestCoverageGap_NoSpansIsFullGap(t *testing.T) {
	want, err := NewTimeRange(
		time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	gap, ok := coverageGap(want, nil)
	assert.False(t, ok)
	assert.Equal(t, want, gap)
}

func TestCoverageGap_GapInMiddleDetected(t *testing.T) {
	want, err := NewTimeRange(
		time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 4, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	first, err := NewTimeRange(
		time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	last, err := NewTimeRange(
		time.Date(2020, 3, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 4, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	// Spans deliberately out of order: coverageGap must sort internally.
	gap, ok := coverageGap(want, []TimeRange{last, first})
	require.False(t, ok)
	assert.Equal(t, time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC), gap.Start())
	assert.Equal(t, time.Date(2020, 3, 3, 0, 0, 0, 0, time.UTC), gap.End())
}

func TestCoverageGap_OverlappingSpansMergeCleanly(t *testing.T) {
	want, err := NewTimeRange(
		time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 3, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	a, err := NewTimeRange(
		time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 2, 12, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	b, err := NewTimeRange(
		time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 3, 3, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	_, ok := coverageGap(want, []TimeRange{a, b})
	assert.True(t, ok)
}

func TestBarReaderNextAfterCloseErrors(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	publishTestPartition(t, mgr, key, m, validBarSet(t))

	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.NoError(t, reader.Close(), "Close must be idempotent")

	_, err = reader.Next(context.Background())
	assert.ErrorIs(t, err, errBarReaderClosed)
}

// A nil *BarReader must not panic: it is a plausible caller mistake (a
// forgotten error check after a failed Bars call), and every method
// treats it as an already-closed, empty reader.
func TestNilBarReaderIsSafe(t *testing.T) {
	var r *BarReader
	_, err := r.Next(context.Background())
	assert.ErrorIs(t, err, errBarReaderClosed)
	assert.Nil(t, r.Manifests())
	assert.NoError(t, r.Close())
}

func TestManagerBars_MalformedPartitionIsNotErrDataUnavailable(t *testing.T) {
	mgr := newTestManager(t)
	key := validPartitionKey(t)
	m := validManifest(t)
	publishTestPartition(t, mgr, key, m, validBarSet(t))

	path, err := key.path(mgr.storeRoot)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("not a canonical partition file\n"), 0o644))

	_, err = mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: m.Span})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrDataUnavailable, "a malformed file is a different failure than missing data")
}
