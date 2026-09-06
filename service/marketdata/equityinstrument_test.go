package marketdata_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

func TestRegisterEquityInstrument_Valid(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	id, err := svc.RegisterEquityInstrument(resolver, svc.EquityRegistration{
		Provider: "alpaca",
		Exchange: "NASDAQ",
		Ticker:   "AAPL",
		Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)
	require.False(t, id.IsZero())

	wantID, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	require.Equal(t, wantID.ID(), id)

	listing, err := resolver.ResolveInstrument(id, "alpaca", "")
	require.NoError(t, err)
	require.Equal(t, "AAPL", listing.Symbol())
	require.Equal(t, "alpaca", listing.Provider())
	require.Equal(t, "0.01", listing.Spec().TickSize().String())
	require.Equal(t, "1", listing.Spec().QuantityIncrement().String())
}

func TestRegisterETFInstrument_Valid(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	id, err := svc.RegisterETFInstrument(resolver, svc.EquityRegistration{
		Provider: "stooq",
		Exchange: "ARCA",
		Ticker:   "SPY",
		Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)

	wantID, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	require.Equal(t, wantID.ID(), id)
}

// TestRegisterEquityInstrument_ETFAndEquityNeverCollide confirms an
// equity and an ETF sharing the identical exchange/ticker resolve to
// distinct instrument.IDs — instrument.EquityID/ETFID's own namespace
// separation, exercised through this service-level registration path
// rather than only instrument's own unit tests. Each is registered
// into its own resolver: a real market never lists a stock and an ETF
// under the same ticker at one provider, and instrument.Resolver's own
// (provider, venue, symbol) uniqueness contract (ADR-016) correctly
// rejects that as a duplicate Listing registration regardless of the
// two Instruments' differing Kind — this test's purpose is the ID
// namespace separation, not exercising that unrelated collision rule.
func TestRegisterEquityInstrument_ETFAndEquityNeverCollide(t *testing.T) {
	eqResolver := instrument.NewMemoryResolver()
	eqID, err := svc.RegisterEquityInstrument(eqResolver, svc.EquityRegistration{
		Provider: "alpaca", Exchange: "ARCA", Ticker: "SPY",
		Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)

	etfResolver := instrument.NewMemoryResolver()
	etfID, err := svc.RegisterETFInstrument(etfResolver, svc.EquityRegistration{
		Provider: "alpaca", Exchange: "ARCA", Ticker: "SPY",
		Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)

	require.NotEqual(t, eqID, etfID)
}

// TestRegisterEquityInstrument_ProviderSymbolDiffersFromTicker confirms
// a provider whose native symbol spelling differs from the canonical
// ticker (Stooq files SPY as "spy.us") registers correctly, with
// Listing.Symbol() reporting the provider's own spelling while
// instrument identity still derives from the canonical ticker.
func TestRegisterEquityInstrument_ProviderSymbolDiffersFromTicker(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	id, err := svc.RegisterETFInstrument(resolver, svc.EquityRegistration{
		Provider:       "stooq",
		Exchange:       "ARCA",
		Ticker:         "SPY",
		ProviderSymbol: "spy.us",
		Currency:       num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)

	wantID, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	require.Equal(t, wantID.ID(), id)

	listing, err := resolver.ResolveInstrument(id, "stooq", "")
	require.NoError(t, err)
	require.Equal(t, "spy.us", listing.Symbol())
}

func TestRegisterEquityInstrument_EmptyProviderSymbolDefaultsToTicker(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	id, err := svc.RegisterEquityInstrument(resolver, svc.EquityRegistration{
		Provider: "alpaca", Exchange: "NASDAQ", Ticker: "aapl",
		Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)

	listing, err := resolver.ResolveInstrument(id, "alpaca", "")
	require.NoError(t, err)
	require.Equal(t, "AAPL", listing.Symbol())
}

func TestRegisterEquityInstrument_RejectsInvalidExchangeOrTicker(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	_, err := svc.RegisterEquityInstrument(resolver, svc.EquityRegistration{
		Provider: "alpaca", Exchange: "", Ticker: "AAPL",
		Currency: num.MustParseCurrency("USD"),
	})
	require.Error(t, err)

	_, err = svc.RegisterEquityInstrument(resolver, svc.EquityRegistration{
		Provider: "alpaca", Exchange: "NASDAQ", Ticker: "",
		Currency: num.MustParseCurrency("USD"),
	})
	require.Error(t, err)
}

func TestRegisterEquityInstrument_RejectsInvalidCurrency(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	_, err := svc.RegisterEquityInstrument(resolver, svc.EquityRegistration{
		Provider: "alpaca", Exchange: "NASDAQ", Ticker: "AAPL",
	})
	require.Error(t, err)
}

// TestFXAndEquityInstrumentsCoexist confirms registering both an FX
// pair and an equity in the same resolver works without either
// registration path assuming it owns the resolver exclusively — the
// cross-asset regression issue #295's own acceptance criteria asks
// for ("existing FX instruments continue to work unchanged").
func TestFXAndEquityInstrumentsCoexist(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	fxID, err := svc.RegisterFXInstrument(resolver, "oanda", "EURUSD")
	require.NoError(t, err)

	eqID, err := svc.RegisterEquityInstrument(resolver, svc.EquityRegistration{
		Provider: "alpaca", Exchange: "NASDAQ", Ticker: "AAPL",
		Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)

	require.NotEqual(t, fxID, eqID)

	fxListing, err := resolver.ResolveInstrument(fxID, "oanda", "")
	require.NoError(t, err)
	require.Equal(t, "EURUSD", fxListing.Symbol())

	eqListing, err := resolver.ResolveInstrument(eqID, "alpaca", "")
	require.NoError(t, err)
	require.Equal(t, "AAPL", eqListing.Symbol())
}
