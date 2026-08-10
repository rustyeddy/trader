package instrument

import (
	"sync"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustListing(t *testing.T, inst Instrument, provider, venue, symbol string) Listing {
	t.Helper()
	l, err := NewListing(ListingParams{
		Instrument: inst,
		Provider:   provider,
		Venue:      venue,
		Symbol:     symbol,
		Spec:       validFXSpec(t),
		Tradable:   !isNeverTradable(inst.Kind()),
	})
	require.NoError(t, err)
	return l
}

// TestEurUsdResolvesForTwoProviderSymbolFormats is the acceptance-
// criterion test: EUR/USD registered under two different providers, each
// with its own symbol spelling, resolves correctly through both.
func TestEurUsdResolvesForTwoProviderSymbolFormats(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()

	oanda := mustListing(t, eurUsd, "OANDA", "", "EUR_USD")
	require.NoError(t, r.Register(oanda))

	other := mustListing(t, eurUsd, "OtherProvider", "", "EURUSD")
	require.NoError(t, r.Register(other))

	got1, err := r.ResolveSymbol("OANDA", "", "EUR_USD")
	require.NoError(t, err)
	assert.True(t, got1.InstrumentID().Equal(eurUsd.ID()))
	assert.Equal(t, "EUR_USD", got1.Symbol())

	got2, err := r.ResolveSymbol("OtherProvider", "", "EURUSD")
	require.NoError(t, err)
	assert.True(t, got2.InstrumentID().Equal(eurUsd.ID()))
	assert.Equal(t, "EURUSD", got2.Symbol())
}

func TestResolveSymbolIsCaseInsensitive(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()
	require.NoError(t, r.Register(mustListing(t, eurUsd, "OANDA", "", "EUR_USD")))

	got, err := r.ResolveSymbol("oanda", "", "eur_usd")
	require.NoError(t, err)
	assert.Equal(t, "EUR_USD", got.Symbol(), "the returned Listing's own fields are never normalized")
}

func TestResolveSymbolUnknown(t *testing.T) {
	r := NewMemoryResolver()
	_, err := r.ResolveSymbol("OANDA", "", "EUR_USD")
	require.ErrorIs(t, err, ErrUnknownSymbol)
}

// TestResolveSymbolAmbiguousAcrossVenues shows that an unconstrained venue
// query is a wildcard, not a filter for a literal empty Venue: when one
// provider exposes the same symbol on two different venues, an
// unconstrained ResolveSymbol call cannot pick one.
func TestResolveSymbolAmbiguousAcrossVenues(t *testing.T) {
	apple, err := NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	r := NewMemoryResolver()

	require.NoError(t, r.Register(mustListing(t, apple, "IBKR", "NASDAQ", "AAPL")))
	require.NoError(t, r.Register(mustListing(t, apple, "IBKR", "BATS", "AAPL")))

	_, err = r.ResolveSymbol("IBKR", "", "AAPL")
	require.ErrorIs(t, err, ErrAmbiguousSymbol)

	got, err := r.ResolveSymbol("IBKR", "NASDAQ", "AAPL")
	require.NoError(t, err)
	assert.Equal(t, "NASDAQ", got.Venue())
}

// TestEmptyVenueQueryIsWildcardNotLiteralMatch confirms that a Listing
// legitimately registered with an empty Venue (spot FX) is still found by
// an unconstrained ("") venue query when it is the only match — "" is a
// wildcard on the query side, not a request to match only literal empty
// Venue values.
func TestEmptyVenueQueryIsWildcardNotLiteralMatch(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()
	require.NoError(t, r.Register(mustListing(t, eurUsd, "OANDA", "", "EUR_USD")))

	got, err := r.ResolveSymbol("OANDA", "", "EUR_USD")
	require.NoError(t, err)
	assert.Equal(t, "", got.Venue())
}

func TestResolveInstrumentUnknown(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()
	_, err := r.ResolveInstrument(eurUsd.ID(), "", "")
	require.ErrorIs(t, err, ErrUnknownSymbol)
}

func TestResolveInstrumentAmbiguousThenNarrowed(t *testing.T) {
	apple, err := NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	r := NewMemoryResolver()

	require.NoError(t, r.Register(mustListing(t, apple, "IBKR", "NASDAQ", "AAPL")))
	require.NoError(t, r.Register(mustListing(t, apple, "AnotherBroker", "NASDAQ", "AAPL.US")))

	_, err = r.ResolveInstrument(apple.ID(), "", "")
	require.ErrorIs(t, err, ErrAmbiguousSymbol)

	got, err := r.ResolveInstrument(apple.ID(), "IBKR", "")
	require.NoError(t, err)
	assert.Equal(t, "AAPL", got.Symbol())

	got, err = r.ResolveInstrument(apple.ID(), "", "NASDAQ")
	require.Error(t, err) // still ambiguous: both are on NASDAQ
	require.ErrorIs(t, err, ErrAmbiguousSymbol)

	got, err = r.ResolveInstrument(apple.ID(), "AnotherBroker", "NASDAQ")
	require.NoError(t, err)
	assert.Equal(t, "AAPL.US", got.Symbol())
}

func TestRegisterRejectsUnconstructedListing(t *testing.T) {
	r := NewMemoryResolver()
	err := r.Register(Listing{})
	require.ErrorIs(t, err, ErrInvalidListing)
}

func TestRegisterRejectsExactDuplicate(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()
	require.NoError(t, r.Register(mustListing(t, eurUsd, "OANDA", "", "EUR_USD")))

	err := r.Register(mustListing(t, eurUsd, "OANDA", "", "EUR_USD"))
	require.ErrorIs(t, err, ErrDuplicateListing)
}

func TestRegisterAllowsSameProviderSymbolOnDifferentVenues(t *testing.T) {
	apple, err := NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	r := NewMemoryResolver()

	require.NoError(t, r.Register(mustListing(t, apple, "IBKR", "NASDAQ", "AAPL")))
	require.NoError(t, r.Register(mustListing(t, apple, "IBKR", "BATS", "AAPL")))
}

// TestRegisterAliasResolvesToUnchangedCanonicalListing is the "aliases do
// not become canonical identity" acceptance criterion: resolving through
// an alias returns the exact canonical Listing value, with its own
// Provider/Venue/Symbol intact, not anything derived from the alias.
func TestRegisterAliasResolvesToUnchangedCanonicalListing(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()
	canonical := mustListing(t, eurUsd, "OANDA", "", "EUR_USD")
	require.NoError(t, r.Register(canonical))

	require.NoError(t, r.RegisterAlias("OANDA", "", "EURUSD", "OANDA", "", "EUR_USD"))

	got, err := r.ResolveSymbol("OANDA", "", "EURUSD")
	require.NoError(t, err)
	assert.Equal(t, canonical, got)
	assert.Equal(t, "EUR_USD", got.Symbol(), "an alias never changes the resolved Listing's own symbol")
}

func TestRegisterAliasRejectsUnknownCanonical(t *testing.T) {
	r := NewMemoryResolver()
	err := r.RegisterAlias("OANDA", "", "EURUSD", "OANDA", "", "EUR_USD")
	require.ErrorIs(t, err, ErrUnknownSymbol)
}

func TestRegisterAliasRejectsAmbiguousCanonical(t *testing.T) {
	apple, err := NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	r := NewMemoryResolver()
	require.NoError(t, r.Register(mustListing(t, apple, "IBKR", "NASDAQ", "AAPL")))
	require.NoError(t, r.Register(mustListing(t, apple, "IBKR", "BATS", "AAPL")))

	err = r.RegisterAlias("IBKR", "", "APPL", "IBKR", "", "AAPL")
	require.ErrorIs(t, err, ErrAmbiguousSymbol)
}

func TestRegisterAliasRejectsDuplicateAliasKey(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()
	require.NoError(t, r.Register(mustListing(t, eurUsd, "OANDA", "", "EUR_USD")))
	require.NoError(t, r.Register(mustListing(t, eurUsd, "OANDA", "", "EURUSD")))

	err := r.RegisterAlias("OANDA", "", "EURUSD", "OANDA", "", "EUR_USD")
	require.ErrorIs(t, err, ErrDuplicateListing)
}

// TestAliasNeverDuplicatesTheInstrumentIndex confirms that registering an
// alias for an already-registered Listing does not make ResolveInstrument
// see two listings where there is only one.
func TestAliasNeverDuplicatesTheInstrumentIndex(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()
	require.NoError(t, r.Register(mustListing(t, eurUsd, "OANDA", "", "EUR_USD")))
	require.NoError(t, r.RegisterAlias("OANDA", "", "EURUSD", "OANDA", "", "EUR_USD"))

	got, err := r.ResolveInstrument(eurUsd.ID(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "EUR_USD", got.Symbol())
}

func TestMultipleResolverInstancesAreIndependent(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r1 := NewMemoryResolver()
	r2 := NewMemoryResolver()

	require.NoError(t, r1.Register(mustListing(t, eurUsd, "OANDA", "", "EUR_USD")))

	_, err := r1.ResolveSymbol("OANDA", "", "EUR_USD")
	require.NoError(t, err)

	_, err = r2.ResolveSymbol("OANDA", "", "EUR_USD")
	require.ErrorIs(t, err, ErrUnknownSymbol, "r2 must not see r1's registrations")
}

// TestConcurrentRegisterAndResolve exercises Register and ResolveSymbol
// from many goroutines at once; it is meaningful under -race.
func TestConcurrentRegisterAndResolve(t *testing.T) {
	r := NewMemoryResolver()
	const n = 50

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			base := num.MustParseCurrency("EUR")
			quote := num.MustParseCurrency("USD")
			inst, err := NewCurrencyPair(base, quote)
			require.NoError(t, err)

			l, err := NewListing(ListingParams{
				Instrument: inst,
				Provider:   "OANDA",
				Symbol:     "EUR_USD",
				Spec:       validFXSpec(t),
				Tradable:   true,
			})
			require.NoError(t, err)

			_ = r.Register(l) // exactly one goroutine's Register succeeds; races are the point under -race
			_, _ = r.ResolveSymbol("OANDA", "", "EUR_USD")
		}(i)
	}
	wg.Wait()

	got, err := r.ResolveSymbol("OANDA", "", "EUR_USD")
	require.NoError(t, err)
	assert.Equal(t, "EUR_USD", got.Symbol())
}
