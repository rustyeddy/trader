package marketdata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/marketdata/internal/provider/alpaca"
	"github.com/rustyeddy/trader/internal/marketdata/internal/provider/stooq"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

func utcHour(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, time.UTC)
}

// publishH1 publishes a canonical EURUSD H1 partition holding one bar
// at each of times (all in one month).
func publishH1(t *testing.T, mgr *Manager, times ...time.Time) {
	t.Helper()
	span, err := marketdata.NewTimeRange(times[0], times[len(times)-1].Add(time.Hour))
	require.NoError(t, err)
	bars := make([]marketdata.Bar, len(times))
	for i, at := range times {
		bars[i] = barAt(t, at)
	}
	publishCanonicalMonth(t, mgr, marketdata.H1, times[0].Year(), times[0].Month(), span, bars, validRawFingerprint, nil)
}

func TestInventory_NoRawNoCanonical(t *testing.T) {
	rawRoot := filepath.Join(t.TempDir(), "never-created")
	mgr := newTestManagerWithRaw(t, rawRoot)

	inv, err := mgr.Inventory(context.Background(), eurusd(), marketdata.H1)
	require.NoError(t, err)
	assert.True(t, inv.Instrument.Equal(eurusd()))
	assert.Equal(t, marketdata.H1, inv.Interval)
	assert.Nil(t, inv.Raw)
	assert.Nil(t, inv.Canonical)
	assert.NoDirExists(t, rawRoot, "Inventory never creates the raw root")
	entries, err := os.ReadDir(mgr.storeRoot)
	require.NoError(t, err)
	assert.Empty(t, entries, "Inventory never writes the store")
}

func TestInventory_OANDARawAheadOfCanonical(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	writeRawPartition(t, rawRoot, "EURUSD", marketdata.H1, 2024, time.February,
		rawRow(utcHour(2024, time.February, 5, 10), true), rawRow(utcHour(2024, time.February, 6, 14), false))
	writeRawPartition(t, rawRoot, "EURUSD", marketdata.H1, 2024, time.January,
		rawRow(utcHour(2024, time.January, 8, 10), true), rawRow(utcHour(2024, time.January, 8, 11), true))
	publishH1(t, mgr, utcHour(2024, time.January, 8, 10), utcHour(2024, time.January, 8, 11))

	inv, err := mgr.Inventory(context.Background(), eurusd(), marketdata.H1)
	require.NoError(t, err)

	require.NotNil(t, inv.Raw)
	assert.Equal(t, 2, inv.Raw.Partitions)
	assert.True(t, inv.Raw.First.Equal(utcHour(2024, time.January, 8, 10)), inv.Raw.First)
	assert.True(t, inv.Raw.Last.Equal(utcHour(2024, time.February, 6, 14)), "an incomplete last record still counts: %s", inv.Raw.Last)

	require.NotNil(t, inv.Canonical)
	assert.Equal(t, 1, inv.Canonical.Partitions)
	assert.True(t, inv.Canonical.First.Equal(utcHour(2024, time.January, 8, 10)))
	assert.True(t, inv.Canonical.Last.Equal(utcHour(2024, time.January, 8, 11)))
}

func TestInventory_RawIgnoresOtherSymbolsIntervalsAndUnhealthyPartitions(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	writeRawPartition(t, rawRoot, "EURUSD", marketdata.H1, 2024, time.March, rawRow(utcHour(2024, time.March, 4, 9), true))
	writeRawPartition(t, rawRoot, "GBPUSD", marketdata.H1, 2023, time.January, rawRow(utcHour(2023, time.January, 9, 9), true))
	writeRawPartition(t, rawRoot, "EURUSD", marketdata.D1, 2022, time.January, rawRow(utcHour(2022, time.January, 3, 22), true))
	writeRawPartition(t, rawRoot, "EURUSD", marketdata.H1, 2024, time.April) // no rows
	writeMalformedRawPartition(t, rawRoot, "EURUSD", "h1", 2025, time.January)

	inv, err := mgr.Inventory(context.Background(), eurusd(), marketdata.H1)
	require.NoError(t, err)
	require.NotNil(t, inv.Raw)
	assert.Equal(t, 1, inv.Raw.Partitions)
	assert.True(t, inv.Raw.First.Equal(utcHour(2024, time.March, 4, 9)))
	assert.True(t, inv.Raw.Last.Equal(utcHour(2024, time.March, 4, 9)))
	assert.Nil(t, inv.Canonical)
}

func TestInventory_OnlyUnhealthyRawIsNoRaw(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	writeMalformedRawPartition(t, rawRoot, "EURUSD", "h1", 2024, time.January)

	inv, err := mgr.Inventory(context.Background(), eurusd(), marketdata.H1)
	require.NoError(t, err)
	assert.Nil(t, inv.Raw)
}

func TestInventory_CanonicalGapsAndInvalidPartitions(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	publishH1(t, mgr, utcHour(2024, time.January, 8, 10))
	publishH1(t, mgr, utcHour(2024, time.March, 4, 9), utcHour(2024, time.March, 4, 12)) // February is a gap
	publishH1(t, mgr, utcHour(2024, time.April, 1, 9))
	publishH1(t, mgr, utcHour(2023, time.December, 4, 9))
	// Corrupt the latest (April) and earliest (December 2023) partitions.
	for _, ym := range []yearMonth{{2024, time.April}, {2023, time.December}} {
		key := partitionKey{provider: "oanda", symbol: "EURUSD", instrument: eurusd(), interval: marketdata.H1, year: ym.year, month: ym.month}
		path, err := key.path(mgr.storeRoot)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte("not a canonical partition\n"), 0o644))
	}

	inv, err := mgr.Inventory(context.Background(), eurusd(), marketdata.H1)
	require.NoError(t, err)
	require.NotNil(t, inv.Canonical)
	assert.Equal(t, 4, inv.Canonical.Partitions, "every published file is counted, valid or not")
	assert.True(t, inv.Canonical.First.Equal(utcHour(2024, time.January, 8, 10)), "skips the invalid earliest partition: %s", inv.Canonical.First)
	assert.True(t, inv.Canonical.Last.Equal(utcHour(2024, time.March, 4, 12)), "skips the invalid latest partition: %s", inv.Canonical.Last)
	assert.Nil(t, inv.Raw)
}

func TestInventory_CanonicalAllInvalidHasZeroTimes(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	publishH1(t, mgr, utcHour(2024, time.January, 8, 10))
	key := partitionKey{provider: "oanda", symbol: "EURUSD", instrument: eurusd(), interval: marketdata.H1, year: 2024, month: time.January}
	path, err := key.path(mgr.storeRoot)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("garbage\n"), 0o644))

	inv, err := mgr.Inventory(context.Background(), eurusd(), marketdata.H1)
	require.NoError(t, err)
	require.NotNil(t, inv.Canonical)
	assert.Equal(t, 1, inv.Canonical.Partitions)
	assert.True(t, inv.Canonical.First.IsZero())
	assert.True(t, inv.Canonical.Last.IsZero())
}

func TestInventory_W1HasNoRawAndNeedsNoRawRoot(t *testing.T) {
	mgr, err := New(Config{Clock: testClock(), StoreRoot: t.TempDir(), Resolver: testResolver(t), ProviderName: "oanda"})
	require.NoError(t, err)
	monthStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	span, err := marketdata.NewTimeRange(monthStart, monthStart.AddDate(0, 1, 0))
	require.NoError(t, err)
	d1 := publishCanonicalMonth(t, mgr, marketdata.D1, 2024, time.January, span, []marketdata.Bar{barAt(t, aWeekday(0))}, validRawFingerprint, nil)
	parent := &marketdata.ParentRef{Instrument: eurusd(), Interval: marketdata.D1, Revision: d1.Revision()}
	publishCanonicalMonth(t, mgr, marketdata.W1, 2024, time.January, span, []marketdata.Bar{barAt(t, aWeekday(0))}, validRawFingerprint, parent)

	inv, err := mgr.Inventory(context.Background(), eurusd(), marketdata.W1)
	require.NoError(t, err)
	assert.Nil(t, inv.Raw)
	require.NotNil(t, inv.Canonical)
	assert.Equal(t, 1, inv.Canonical.Partitions)
	assert.True(t, inv.Canonical.First.Equal(aWeekday(0)))
}

func TestInventory_StooqRawAndCanonical(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()
	_, err := stooq.Import(ctx, filepath.Join("internal", "provider", "stooq", "testdata", "spy_us_d_sample.csv"), rawRoot, "SPY")
	require.NoError(t, err)
	mgr := newStooqTestManager(t, rawRoot)
	first := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)

	inv, err := mgr.Inventory(ctx, spyID(t), marketdata.D1)
	require.NoError(t, err)
	require.NotNil(t, inv.Raw)
	assert.Equal(t, 2, inv.Raw.Partitions)
	assert.True(t, inv.Raw.First.Equal(first), inv.Raw.First)
	assert.True(t, inv.Raw.Last.Equal(last), inv.Raw.Last)
	assert.Nil(t, inv.Canonical, "nothing built yet")

	span, err := marketdata.NewTimeRange(first, last.AddDate(0, 0, 1))
	require.NoError(t, err)
	plan, err := mgr.Plan(ctx, BarQuery{Instrument: spyID(t), Interval: marketdata.D1, Range: span})
	require.NoError(t, err)
	_, err = mgr.Build(ctx, plan)
	require.NoError(t, err)

	inv, err = mgr.Inventory(ctx, spyID(t), marketdata.D1)
	require.NoError(t, err)
	require.NotNil(t, inv.Canonical)
	assert.Equal(t, 2, inv.Canonical.Partitions)
	assert.True(t, inv.Canonical.First.Equal(first), inv.Canonical.First)
	assert.True(t, inv.Canonical.Last.Equal(last), inv.Canonical.Last)

	_, err = mgr.Inventory(ctx, spyID(t), marketdata.H1)
	assert.ErrorContains(t, err, "only D1 is supported", "stooq holds no H1 raw data")
}

func TestInventory_AlpacaRaw(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()
	rec := func(at time.Time) alpaca.Record {
		return alpaca.Record{Time: at, Open: num.MustParsePrice("1"), High: num.MustParsePrice("2"),
			Low: num.MustParsePrice("0.5"), Close: num.MustParsePrice("1.5"), Volume: 10}
	}
	require.NoError(t, alpaca.WritePartition(ctx, rawRoot, "SPY", 2020, time.May, alpaca.FeedIEX,
		[]alpaca.Record{rec(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)), rec(time.Date(2020, 5, 4, 0, 0, 0, 0, time.UTC))}, true))
	require.NoError(t, alpaca.WritePartition(ctx, rawRoot, "AAPL", 2021, time.May, alpaca.FeedIEX,
		[]alpaca.Record{rec(time.Date(2021, 5, 3, 0, 0, 0, 0, time.UTC))}, true))
	mgr := newAlpacaTestManager(t, rawRoot)

	inv, err := mgr.Inventory(ctx, alpacaSPYID(t), marketdata.D1)
	require.NoError(t, err)
	require.NotNil(t, inv.Raw)
	assert.Equal(t, 1, inv.Raw.Partitions)
	assert.True(t, inv.Raw.First.Equal(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)), inv.Raw.First)
	assert.True(t, inv.Raw.Last.Equal(time.Date(2020, 5, 4, 0, 0, 0, 0, time.UTC)), inv.Raw.Last)
	assert.Nil(t, inv.Canonical)
}

func TestInventory_Errors(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManagerWithRaw(t, t.TempDir())

	var unconfigured Manager
	_, err := unconfigured.Inventory(ctx, eurusd(), marketdata.H1)
	assert.ErrorIs(t, err, ErrInvalidConfig)

	_, err = mgr.Inventory(ctx, instrument.ID{}, marketdata.H1)
	assert.ErrorIs(t, err, ErrInvalidQuery)

	_, err = mgr.Inventory(ctx, eurusd(), marketdata.Interval{})
	assert.ErrorIs(t, err, ErrInvalidQuery)

	_, err = mgr.Inventory(ctx, gbpusd(), marketdata.H1)
	assert.ErrorIs(t, err, instrument.ErrUnknownSymbol)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = mgr.Inventory(canceled, eurusd(), marketdata.H1)
	assert.ErrorIs(t, err, context.Canceled)

	noRaw, err := New(Config{Clock: testClock(), StoreRoot: t.TempDir(), Resolver: testResolver(t), ProviderName: "oanda"})
	require.NoError(t, err)
	_, err = noRaw.Inventory(ctx, eurusd(), marketdata.H1)
	assert.ErrorIs(t, err, ErrInvalidConfig, "a raw-built interval needs a raw root")
}

func TestCanonicalCSVStoreMonths(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManagerWithRaw(t, t.TempDir())
	publishH1(t, mgr, utcHour(2024, time.March, 4, 9))
	publishH1(t, mgr, utcHour(2023, time.December, 4, 9))
	publishH1(t, mgr, utcHour(2024, time.January, 8, 9))
	root := mgr.storeRoot
	// Noise the listing must ignore.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "oanda", "EURUSD", "2024", "02"), 0o755)) // month dir without a file
	require.NoError(t, os.MkdirAll(filepath.Join(root, "oanda", "EURUSD", "notes", "01"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "oanda", "EURUSD", "2024", "13"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "oanda", "EURUSD", "README"), nil, 0o644))

	store := newCanonicalCSVStore(root)
	got, err := store.months(ctx, "oanda", "EURUSD", marketdata.H1)
	require.NoError(t, err)
	assert.Equal(t, []yearMonth{{2023, time.December}, {2024, time.January}, {2024, time.March}}, got)

	got, err = store.months(ctx, "oanda", "EURUSD", marketdata.D1)
	require.NoError(t, err)
	assert.Empty(t, got, "other intervals' files are not listed")

	got, err = store.months(ctx, "oanda", "USDJPY", marketdata.H1)
	require.NoError(t, err)
	assert.Empty(t, got, "a missing tree is an empty listing")

	_, err = store.months(ctx, "oanda", "../x", marketdata.H1)
	assert.ErrorIs(t, err, errStoreInvalidPartitionKey)
	_, err = store.months(ctx, "", "EURUSD", marketdata.H1)
	assert.ErrorIs(t, err, errStoreInvalidPartitionKey)
	_, err = store.months(ctx, "oanda", "EURUSD", marketdata.Interval{})
	assert.ErrorIs(t, err, errStoreUnsupportedInterval)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.months(canceled, "oanda", "EURUSD", marketdata.H1)
	assert.ErrorIs(t, err, context.Canceled)
}
