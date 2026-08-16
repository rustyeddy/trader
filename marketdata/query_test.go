package marketdata

import (
	"context"
	"errors"
	"io"
	"os"
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

	marchKey := validPartitionKey(t)
	marchManifest := validManifest(t)
	marchBars := validBarSet(t)
	publishTestPartition(t, mgr, marchKey, marchManifest, marchBars)

	aprilStart := time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC)
	aprilEnd := time.Date(2020, 4, 1, 2, 0, 0, 0, time.UTC)
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
