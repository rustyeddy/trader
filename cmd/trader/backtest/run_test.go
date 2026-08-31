package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// TestNextBarOpenAfterEntry_UsesFollowingBarOpenNotEntryBarClose is the
// PR #240 review regression: the committed vertical-slice fixture
// happens to have entry-bar-close == fill-bar-open (a continuous
// synthetic series), which let an earlier version of this file's bug
// — pricing every fill at the entry bar's own Close — pass unnoticed.
// This fixture (testdata/raw/oanda/EURUSD/2024/02) is deliberately
// gapped: 2024-02-01T00:00's bid close (1.20010) differs from
// 2024-02-01T01:00's bid open (1.21000), so a wrong implementation
// returning the entry bar's Close is distinguishable from the correct
// next-bar-open value by more than floating-point noise.
func TestNextBarOpenAfterEntry_UsesFollowingBarOpenNotEntryBarClose(t *testing.T) {
	manager, instrumentID := newGappedFixtureManager(t)
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 3, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Ground truth, read independently of the function under test.
	reader, err := manager.Bars(ctx, marketdata.BarQuery{Instrument: instrumentID, Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	entryBar, err := reader.Next(ctx)
	require.NoError(t, err)
	fillBar, err := reader.Next(ctx)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.False(t, entryBar.Close.Equal(fillBar.Open), "fixture must be gapped for this regression to be meaningful")

	got, err := nextBarOpenAfterEntry(ctx, manager, instrumentID, marketdata.H1, span, 0)
	require.NoError(t, err)

	require.True(t, got.Equal(fillBar.Open), "expected the fill bar's own Open (%s), got %s", fillBar.Open, got)
	require.False(t, got.Equal(entryBar.Close), "must not price the fill at the entry bar's Close (%s)", entryBar.Close)
}

// TestNextBarOpenAfterEntry_AccountsForWarmupBars proves warmupBars
// shifts both the entry bar and the fill bar forward by the same
// amount: with warmupBars=1, the strategy's first delivered OnBar is
// index 1 (2024-02-01T01:00), so its fill bar is index 2
// (2024-02-01T02:00).
func TestNextBarOpenAfterEntry_AccountsForWarmupBars(t *testing.T) {
	manager, instrumentID := newGappedFixtureManager(t)
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 3, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	ctx := context.Background()

	reader, err := manager.Bars(ctx, marketdata.BarQuery{Instrument: instrumentID, Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	_, err = reader.Next(ctx) // index 0, consumed as warm-up
	require.NoError(t, err)
	_, err = reader.Next(ctx) // index 1, the entry bar with warmupBars=1
	require.NoError(t, err)
	fillBar, err := reader.Next(ctx) // index 2, the expected fill bar
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	got, err := nextBarOpenAfterEntry(ctx, manager, instrumentID, marketdata.H1, span, 1)
	require.NoError(t, err)
	require.True(t, got.Equal(fillBar.Open))
}

// newGappedFixtureManager returns a *marketdata.Manager over
// testdata/raw/oanda's deliberately gapped February fixture.
func newGappedFixtureManager(t *testing.T) (*marketdata.Manager, instrument.ID) {
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
		RawRoot:      "testdata/raw/oanda",
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 3, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	ctx := context.Background()
	plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: listing.InstrumentID(), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	return manager, listing.InstrumentID()
}
