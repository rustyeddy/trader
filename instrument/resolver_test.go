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

// TestResolveSymbolRequiresProviderAndSymbol pins the corrected contract:
// unlike venue, provider and symbol are not wildcardable on ResolveSymbol.
// An earlier draft's doc comment claimed otherwise while the
// implementation only ever wildcarded venue — "" for provider or symbol
// reports ErrInvalidResolution rather than silently searching a
// nonsensical empty-provider or empty-symbol bucket.
func TestResolveSymbolRequiresProviderAndSymbol(t *testing.T) {
	eurUsd := mustEurUsd(t)
	r := NewMemoryResolver()
	require.NoError(t, r.Register(mustListing(t, eurUsd, "OANDA", "", "EUR_USD")))

	_, err := r.ResolveSymbol("", "", "EUR_USD")
	require.ErrorIs(t, err, ErrInvalidResolution)

	_, err = r.ResolveSymbol("OANDA", "", "")
	require.ErrorIs(t, err, ErrInvalidResolution)
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

	_, err = r.ResolveInstrument(apple.ID(), "", "NASDAQ")
	require.Error(t, err) // still ambiguous: both are on NASDAQ
	require.ErrorIs(t, err, ErrAmbiguousSymbol)

	got, err = r.ResolveInstrument(apple.ID(), "AnotherBroker", "NASDAQ")
	require.NoError(t, err)
	assert.Equal(t, "AAPL.US", got.Symbol())
}

// TestResolveInstrumentOnlyReturnsOrderableListings is the "resolve an
// instrument and venue to an orderable listing" acceptance criterion:
// ResolveInstrument must filter to Tradable listings, not merely narrow by
// provider/venue among all registered listings regardless of tradability.
func TestResolveInstrumentOnlyReturnsOrderableListings(t *testing.T) {
	inst, err := NewContinuousSeries("ES")
	require.NoError(t, err)
	r := NewMemoryResolver()

	nonTradable, err := NewListing(ListingParams{
		Instrument: inst,
		Provider:   "Trader Research",
		Symbol:     "ES-CONT",
		Spec:       validFXSpec(t),
		Tradable:   false,
	})
	require.NoError(t, err)
	require.NoError(t, r.Register(nonTradable))

	// The only registered listing for this instrument is non-tradable, so
	// there is nothing orderable to resolve to.
	_, err = r.ResolveInstrument(inst.ID(), "", "")
	require.ErrorIs(t, err, ErrUnknownSymbol)

	// ResolveSymbol makes no orderable promise, so the same listing is
	// still reachable by its own symbol.
	got, err := r.ResolveSymbol("Trader Research", "", "ES-CONT")
	require.NoError(t, err)
	assert.False(t, got.Tradable())
}

// TestResolveInstrumentIgnoresNonTradableListingsWhenDisambiguating shows
// that a non-tradable listing sharing an instrument and venue with a
// tradable one never contributes to ambiguity: without the Tradable
// filter, these two registrations would make an unnarrowed-by-provider
// lookup ambiguous even though only one of them is actually orderable.
func TestResolveInstrumentIgnoresNonTradableListingsWhenDisambiguating(t *testing.T) {
	apple, err := NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	r := NewMemoryResolver()

	tradable, err := NewListing(ListingParams{
		Instrument: apple,
		Provider:   "IBKR",
		Venue:      "NASDAQ",
		Symbol:     "AAPL",
		Spec:       validFXSpec(t),
		Tradable:   true,
	})
	require.NoError(t, err)
	require.NoError(t, r.Register(tradable))

	// Halted at this venue through this second provider -- still a real
	// registered listing of the same instrument and venue, just not
	// currently orderable.
	nonTradable, err := NewListing(ListingParams{
		Instrument: apple,
		Provider:   "AnotherBroker",
		Venue:      "NASDAQ",
		Symbol:     "AAPL",
		Spec:       validFXSpec(t),
		Tradable:   false,
	})
	require.NoError(t, err)
	require.NoError(t, r.Register(nonTradable))

	got, err := r.ResolveInstrument(apple.ID(), "", "NASDAQ")
	require.NoError(t, err)
	assert.Equal(t, "IBKR", got.Provider())
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
