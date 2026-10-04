package marketdata_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/logging"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// newResolvingService is a Service for provider whose resolver is wired
// (WithResolver) exactly as composition roots wire it.
func newResolvingService(t *testing.T, provider string, opts ...svc.Option) (*svc.Service, *instrument.MemoryResolver) {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	cfg := marketruntime.Config{
		Clock: clock.NewSimulated(time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)), StoreRoot: t.TempDir(),
		RawRoot: t.TempDir(), Resolver: resolver, ProviderName: provider,
	}
	if info, err := marketruntime.LookupProvider(provider); err == nil && info.Calendar == marketruntime.CalendarUSEquity {
		cfg.Calendar = marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(2020))
	}
	manager, err := marketruntime.New(cfg)
	require.NoError(t, err)
	s, err := svc.New(manager, nil, append([]svc.Option{svc.WithResolver(resolver)}, opts...)...)
	require.NoError(t, err)
	return s, resolver
}

func TestIdentifyInstrument_RejectsUnknownProvider(t *testing.T) {
	for _, provider := range []string{"", "bloomberg"} {
		_, err := svc.IdentifyInstrument(provider, svc.InstrumentRequest{Symbol: "SPY"})
		assert.ErrorIs(t, err, marketruntime.ErrUnknownProvider, "never guessing an asset class for %q", provider)
	}
	// Each registered provider identifies by its registered asset class.
	for _, info := range marketruntime.Providers() {
		symbol, kind := "SPY", svc.KindETF
		if info.AssetClass == marketruntime.AssetClassFX {
			symbol, kind = "EURUSD", svc.KindFX
		}
		id, err := svc.IdentifyInstrument(info.Name, svc.InstrumentRequest{Symbol: symbol})
		require.NoError(t, err, info.Name)
		assert.Equal(t, kind, id.Kind, info.Name)
	}
}

func TestIdentifyInstrument(t *testing.T) {
	tests := map[string]struct {
		provider string
		req      svc.InstrumentRequest
		want     svc.InstrumentIdentity
	}{
		"fx ignores exchange and kind": {"oanda", svc.InstrumentRequest{Symbol: " eurusd ", Exchange: "ARCA", Kind: "etf"}, svc.InstrumentIdentity{Symbol: "EURUSD", Kind: svc.KindFX}},
		"equity default":               {"stooq", svc.InstrumentRequest{Symbol: "aapl"}, svc.InstrumentIdentity{Symbol: "AAPL", Kind: svc.KindEquity, Exchange: "NASDAQ"}},
		"etf default":                  {"alpaca", svc.InstrumentRequest{Symbol: "SPY"}, svc.InstrumentIdentity{Symbol: "SPY", Kind: svc.KindETF, Exchange: "ARCA"}},
		"explicit identity":            {"alpaca", svc.InstrumentRequest{Symbol: "MSFT", Exchange: "NASDAQ", Kind: "Equity"}, svc.InstrumentIdentity{Symbol: "MSFT", Kind: svc.KindEquity, Exchange: "NASDAQ"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := svc.IdentifyInstrument(tc.provider, tc.req)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	failures := map[string]struct {
		req svc.InstrumentRequest
		is  error
	}{
		"empty symbol":     {svc.InstrumentRequest{Symbol: "  "}, svc.ErrInvalidRequest},
		"no default":       {svc.InstrumentRequest{Symbol: "MSFT"}, svc.ErrNoListingDefault},
		"incomplete":       {svc.InstrumentRequest{Symbol: "MSFT", Kind: "equity"}, svc.ErrIncompleteListingIdentity},
		"unsupported kind": {svc.InstrumentRequest{Symbol: "ES", Exchange: "CME", Kind: "future"}, svc.ErrInvalidListingKind},
	}
	for name, tc := range failures {
		t.Run(name, func(t *testing.T) {
			_, err := svc.IdentifyInstrument("stooq", tc.req)
			assert.ErrorIs(t, err, tc.is)
		})
	}
}

func TestRegisterIdentity(t *testing.T) {
	t.Run("registers under any provider, idempotently", func(t *testing.T) {
		resolver := instrument.NewMemoryResolver()
		id := svc.InstrumentIdentity{Symbol: "SPY", Kind: svc.KindETF, Exchange: "ARCA"}

		data, err := svc.RegisterIdentity(resolver, "stooq", id)
		require.NoError(t, err)
		sim, err := svc.RegisterIdentity(resolver, "sim", id)
		require.NoError(t, err)
		again, err := svc.RegisterIdentity(resolver, "stooq", id)
		require.NoError(t, err)

		assert.True(t, data.InstrumentID().Equal(instrument.ETFID("ARCA", "SPY")))
		assert.True(t, sim.InstrumentID().Equal(data.InstrumentID()))
		assert.Equal(t, "sim", sim.Provider())
		assert.Equal(t, "ARCA", data.Venue())
		assert.True(t, again.InstrumentID().Equal(data.InstrumentID()))
		listings, err := resolver.ResolveInstrument(data.InstrumentID(), "stooq", "")
		require.NoError(t, err, "still exactly one stooq listing")
		assert.Equal(t, "SPY", listings.Symbol())
	})
	t.Run("fx identity", func(t *testing.T) {
		listing, err := svc.RegisterIdentity(instrument.NewMemoryResolver(), "oanda", svc.InstrumentIdentity{Symbol: "USDJPY", Kind: svc.KindFX})
		require.NoError(t, err)
		assert.Equal(t, "USDJPY", listing.Symbol())
		assert.Equal(t, "0.001", listing.Spec().TickSize().String())
	})
	t.Run("equity identity", func(t *testing.T) {
		listing, err := svc.RegisterIdentity(instrument.NewMemoryResolver(), "alpaca", svc.InstrumentIdentity{Symbol: "AAPL", Kind: svc.KindEquity, Exchange: "NASDAQ"})
		require.NoError(t, err)
		assert.True(t, listing.InstrumentID().Equal(instrument.EquityID("NASDAQ", "AAPL")))
	})
	t.Run("conflicting listing under the same key", func(t *testing.T) {
		resolver := instrument.NewMemoryResolver()
		_, err := svc.RegisterETFInstrument(resolver, svc.EquityRegistration{
			Provider: "stooq", Exchange: "ARCA", Ticker: "SPY", Currency: num.MustParseCurrency("USD"),
		})
		require.NoError(t, err)
		_, err = svc.RegisterIdentity(resolver, "stooq", svc.InstrumentIdentity{Symbol: "SPY", Kind: svc.KindEquity, Exchange: "ARCA"})
		require.ErrorIs(t, err, svc.ErrListingConflict, "SPY as an equity is a different instrument from SPY the ETF")
		assert.ErrorIs(t, err, instrument.ErrDuplicateListing)
	})
	t.Run("fx re-registration with the same symbol on another venue", func(t *testing.T) {
		// PR #449 review: an empty venue is a wildcard to ResolveSymbol,
		// so a lookup-based fallback reported this as a conflict.
		resolver := instrument.NewMemoryResolver()
		fx := svc.InstrumentIdentity{Symbol: "EURUSD", Kind: svc.KindFX}
		first, err := svc.RegisterIdentity(resolver, "oanda", fx)
		require.NoError(t, err)
		_, err = svc.RegisterETFInstrument(resolver, svc.EquityRegistration{
			Provider: "oanda", Exchange: "ARCA", Ticker: "EURUSD", Currency: num.MustParseCurrency("USD"),
		})
		require.NoError(t, err)

		again, err := svc.RegisterIdentity(resolver, "oanda", fx)
		require.NoError(t, err)
		assert.True(t, again.InstrumentID().Equal(first.InstrumentID()))
		assert.Equal(t, "", again.Venue())
	})
	t.Run("nil resolver", func(t *testing.T) {
		_, err := svc.RegisterIdentity(nil, "oanda", svc.InstrumentIdentity{Symbol: "EURUSD", Kind: svc.KindFX})
		assert.ErrorIs(t, err, svc.ErrResolverNotConfigured)
	})
	t.Run("unsupported kind", func(t *testing.T) {
		_, err := svc.RegisterIdentity(instrument.NewMemoryResolver(), "stooq", svc.InstrumentIdentity{Symbol: "ES", Kind: "future"})
		assert.ErrorIs(t, err, svc.ErrInvalidRequest)
	})
	t.Run("invalid fx symbol", func(t *testing.T) {
		_, err := svc.RegisterIdentity(instrument.NewMemoryResolver(), "oanda", svc.InstrumentIdentity{Symbol: "EUR", Kind: svc.KindFX})
		assert.ErrorContains(t, err, "6-letter FX pair")
	})
	t.Run("invalid exchange", func(t *testing.T) {
		_, err := svc.RegisterIdentity(instrument.NewMemoryResolver(), "stooq", svc.InstrumentIdentity{Symbol: "SPY", Kind: svc.KindETF, Exchange: "A/B"})
		assert.Error(t, err)
	})
}

func TestRegisterInstrument(t *testing.T) {
	resolver := instrument.NewMemoryResolver()
	listing, err := svc.RegisterInstrument(resolver, "stooq", svc.InstrumentRequest{Symbol: "qqq"})
	require.NoError(t, err)
	assert.True(t, listing.InstrumentID().Equal(instrument.ETFID("NASDAQ", "QQQ")))

	_, err = svc.RegisterInstrument(resolver, "stooq", svc.InstrumentRequest{Symbol: "MSFT"})
	assert.ErrorIs(t, err, svc.ErrNoListingDefault)
}

func TestService_ResolveInstrument(t *testing.T) {
	ctx := context.Background()
	s, resolver := newResolvingService(t, "stooq")
	assert.Equal(t, "stooq", s.Provider())

	resp, err := s.ResolveInstrument(ctx, svc.InstrumentRequest{Symbol: "spy"})
	require.NoError(t, err)
	assert.Equal(t, svc.InstrumentIdentity{Symbol: "SPY", Kind: svc.KindETF, Exchange: "ARCA"}, resp.Identity)
	assert.True(t, resp.Instrument.Equal(instrument.ETFID("ARCA", "SPY")))
	assert.Equal(t, "stooq", resp.Listing.Provider())

	// Registered into the Manager's own resolver, so other operations
	// can act on the instrument.
	_, err = resolver.ResolveInstrument(resp.Instrument, "stooq", "")
	require.NoError(t, err)
	inv, err := s.Inventory(ctx, svc.InventoryRequest{Instrument: resp.Instrument, Interval: marketdata.D1})
	require.NoError(t, err)
	assert.Nil(t, inv.Inventory.Raw)

	again, err := s.ResolveInstrument(ctx, svc.InstrumentRequest{Symbol: "SPY"})
	require.NoError(t, err, "calling twice on the same service succeeds")
	assert.True(t, again.Instrument.Equal(resp.Instrument))

	_, err = s.ResolveInstrument(ctx, svc.InstrumentRequest{Symbol: "MSFT"})
	assert.ErrorIs(t, err, svc.ErrNoListingDefault)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.ResolveInstrument(canceled, svc.InstrumentRequest{Symbol: "SPY"})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestService_ResolveInstrumentRequiresResolver(t *testing.T) {
	s := newTestService(t) // constructed without WithResolver
	_, err := s.ResolveInstrument(context.Background(), svc.InstrumentRequest{Symbol: "EURUSD"})
	assert.ErrorIs(t, err, svc.ErrResolverNotConfigured)
	_, err = s.ResolveInstruments(context.Background(), svc.ResolveInstrumentsRequest{Instruments: []svc.InstrumentRequest{{Symbol: "EURUSD"}}})
	assert.ErrorIs(t, err, svc.ErrResolverNotConfigured)
}

func TestService_ResolveInstruments(t *testing.T) {
	ctx := context.Background()
	logger, rec := logging.Capture()
	resolver := instrument.NewMemoryResolver()
	manager, err := marketruntime.New(marketruntime.Config{
		Clock: clock.NewSimulated(time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)), StoreRoot: t.TempDir(),
		RawRoot: t.TempDir(), Resolver: resolver, ProviderName: "oanda",
	})
	require.NoError(t, err)
	s, err := svc.New(manager, logger, svc.WithResolver(resolver))
	require.NoError(t, err)

	resp, err := s.ResolveInstruments(ctx, svc.ResolveInstrumentsRequest{Instruments: []svc.InstrumentRequest{
		{Symbol: "EURUSD"}, {Symbol: "eurusd"}, {Symbol: "BAD"}, {Symbol: "USDJPY"},
	}})
	require.NoError(t, err, "one bad symbol does not fail the call")
	require.Len(t, resp.Results, 4)
	assert.NoError(t, resp.Results[0].Err)
	assert.NoError(t, resp.Results[1].Err, "a duplicate symbol succeeds")
	assert.True(t, resp.Results[0].Instrument.Equal(resp.Results[1].Instrument))
	assert.Error(t, resp.Results[2].Err)
	assert.Equal(t, "BAD", resp.Results[2].Request.Symbol)
	assert.NoError(t, resp.Results[3].Err)
	assert.Equal(t, "USDJPY", resp.Results[3].Identity.Symbol)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "instruments resolved", records[0].Message)
	assert.Equal(t, int64(4), records[0].Attrs["requested"])
	assert.Equal(t, int64(1), records[0].Attrs["failed"])

	_, err = s.ResolveInstruments(ctx, svc.ResolveInstrumentsRequest{})
	assert.ErrorIs(t, err, svc.ErrInvalidRequest)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.ResolveInstruments(canceled, svc.ResolveInstrumentsRequest{Instruments: []svc.InstrumentRequest{{Symbol: "EURUSD"}}})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestConvertStooqArchive_DefaultsToServiceArchiveRoot(t *testing.T) {
	archiveRoot := t.TempDir()
	writeZIP(t, filepath.Join(archiveRoot, "spy_us_d.zip"), map[string]string{"spy.us.txt": spyTwoMonths})
	s, _ := newResolvingService(t, "stooq", svc.WithArchiveRoot(archiveRoot))
	resp, err := s.ResolveInstrument(context.Background(), svc.InstrumentRequest{Symbol: "SPY"})
	require.NoError(t, err)

	out, err := s.ConvertStooqArchive(context.Background(), svc.ConvertStooqArchiveRequest{
		DatasetRequest: svc.DatasetRequest{Instrument: resp.Instrument, Interval: marketdata.D1},
		Symbol:         "SPY",
	})
	require.NoError(t, err, "no archive path or root in the request: the service's root is searched")
	assert.Equal(t, 2, out.Import.RowsImported)
}

func TestService_ResolveInstrumentLogsOnce(t *testing.T) {
	ctx := context.Background()
	logger, rec := logging.Capture()
	resolver := instrument.NewMemoryResolver()
	manager, err := marketruntime.New(marketruntime.Config{
		Clock: clock.NewSimulated(time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)), StoreRoot: t.TempDir(),
		RawRoot: t.TempDir(), Resolver: resolver, ProviderName: "oanda",
	})
	require.NoError(t, err)
	s, err := svc.New(manager, logger, svc.WithResolver(resolver))
	require.NoError(t, err)

	resp, err := s.ResolveInstrument(ctx, svc.InstrumentRequest{Symbol: "EURUSD"})
	require.NoError(t, err)
	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "instrument resolved", records[0].Message)
	assert.Equal(t, resp.Instrument.String(), records[0].Attrs[logging.InstrumentID])
	assert.Equal(t, "oanda", records[0].Attrs["provider"])
	assert.NotContains(t, records[0].Attrs, "error")

	rec.Reset()
	_, err = s.ResolveInstrument(ctx, svc.InstrumentRequest{Symbol: "BAD"})
	require.Error(t, err)
	records = rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "instrument resolution failed", records[0].Message)
	assert.Contains(t, records[0].Attrs, "error")

	rec.Reset()
	_, err = s.ResolveInstrument(ctx, svc.InstrumentRequest{Symbol: " "})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
	assert.Empty(t, rec.Records(), "an invalid request is not logged")
}

func TestService_ResolveInstrumentsLogsEveryExitAfterValidation(t *testing.T) {
	ctx := context.Background()
	newService := func(t *testing.T, withResolver bool) (*svc.Service, *logging.Recorder) {
		logger, rec := logging.Capture()
		resolver := instrument.NewMemoryResolver()
		manager, err := marketruntime.New(marketruntime.Config{
			Clock: clock.NewSimulated(time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)), StoreRoot: t.TempDir(),
			RawRoot: t.TempDir(), Resolver: resolver, ProviderName: "oanda",
		})
		require.NoError(t, err)
		var opts []svc.Option
		if withResolver {
			opts = append(opts, svc.WithResolver(resolver))
		}
		s, err := svc.New(manager, logger, opts...)
		require.NoError(t, err)
		return s, rec
	}
	one := svc.ResolveInstrumentsRequest{Instruments: []svc.InstrumentRequest{{Symbol: "EURUSD"}}}

	t.Run("missing resolver", func(t *testing.T) {
		s, rec := newService(t, false)
		_, err := s.ResolveInstruments(ctx, one)
		require.ErrorIs(t, err, svc.ErrResolverNotConfigured)
		records := rec.Records()
		require.Len(t, records, 1)
		assert.Equal(t, "instrument resolution failed", records[0].Message)
	})
	t.Run("canceled", func(t *testing.T) {
		s, rec := newService(t, true)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := s.ResolveInstruments(canceled, one)
		require.ErrorIs(t, err, context.Canceled)
		records := rec.Records()
		require.Len(t, records, 1)
		assert.Equal(t, "instrument resolution failed", records[0].Message)
	})
	t.Run("empty request is not logged", func(t *testing.T) {
		s, rec := newService(t, true)
		_, err := s.ResolveInstruments(ctx, svc.ResolveInstrumentsRequest{})
		require.ErrorIs(t, err, svc.ErrInvalidRequest)
		assert.Empty(t, rec.Records())
	})
}
