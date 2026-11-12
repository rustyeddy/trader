package instrument

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustEurUsd(t *testing.T) Instrument {
	t.Helper()
	inst, err := NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	return inst
}

func TestNewListing(t *testing.T) {
	inst := mustEurUsd(t)
	spec := validFXSpec(t)

	l, err := NewListing(ListingParams{
		Instrument: inst,
		Provider:   "OANDA",
		Venue:      "",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)

	assert.True(t, l.InstrumentID().Equal(inst.ID()))
	assert.Equal(t, "OANDA", l.Provider())
	assert.Equal(t, "", l.Venue(), "spot FX has no meaningful centralized venue")
	assert.Equal(t, "EUR_USD", l.Symbol())
	assert.True(t, l.Tradable())
	assert.Equal(t, spec, l.Spec())
}

func TestNewListingDistinguishesProviderFromVenue(t *testing.T) {
	inst, err := NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	spec := validFXSpec(t)

	l, err := NewListing(ListingParams{
		Instrument: inst,
		Provider:   "IBKR",
		Venue:      "NASDAQ",
		Symbol:     "AAPL",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)

	assert.Equal(t, "IBKR", l.Provider())
	assert.Equal(t, "NASDAQ", l.Venue())
}

func TestNewListingPreservesSymbolCaseAndTrimsWhitespace(t *testing.T) {
	inst, err := NewIndex("SPX")
	require.NoError(t, err)
	spec := validFXSpec(t)

	l, err := NewListing(ListingParams{
		Instrument: inst,
		Provider:   "  Bloomberg  ",
		Venue:      "  NASDAQ  ",
		Symbol:     "  spx.us  ",
		Spec:       spec,
	})
	require.NoError(t, err)

	assert.Equal(t, "Bloomberg", l.Provider())
	assert.Equal(t, "NASDAQ", l.Venue())
	assert.Equal(t, "spx.us", l.Symbol(), "provider symbol text is display-only and never normalized")
}

func TestNewListingRejectsUnconstructedInstrument(t *testing.T) {
	_, err := NewListing(ListingParams{
		Provider: "OANDA",
		Symbol:   "EUR_USD",
		Spec:     validFXSpec(t),
	})
	require.ErrorIs(t, err, ErrInvalidListing)
}

func TestNewListingRejectsEmptyProviderOrSymbol(t *testing.T) {
	inst, err := NewIndex("SPX")
	require.NoError(t, err)
	spec := validFXSpec(t)

	_, err = NewListing(ListingParams{Instrument: inst, Provider: "", Symbol: "SPX", Spec: spec})
	require.ErrorIs(t, err, ErrInvalidListing)

	_, err = NewListing(ListingParams{Instrument: inst, Provider: "Bloomberg", Symbol: "", Spec: spec})
	require.ErrorIs(t, err, ErrInvalidListing)
}

func TestNewListingRejectsUnconstructedSpec(t *testing.T) {
	inst, err := NewIndex("SPX")
	require.NoError(t, err)

	_, err = NewListing(ListingParams{
		Instrument: inst,
		Provider:   "Bloomberg",
		Symbol:     "SPX",
		Spec:       Spec{},
	})
	require.ErrorIs(t, err, ErrInvalidListing)
}

// TestNewListingRejectsTradableContinuousSeries is the acceptance-
// criterion test: NewListing mechanically rejects the contradictory
// combination the package doc comment describes — Tradable: true for an
// instrument that is non-orderable by Kind.
func TestNewListingRejectsTradableContinuousSeries(t *testing.T) {
	inst, err := NewContinuousSeries("ES")
	require.NoError(t, err)

	_, err = NewListing(ListingParams{
		Instrument: inst,
		Provider:   "Trader Research",
		Symbol:     "ES-CONT",
		Spec:       validFXSpec(t),
		Tradable:   true,
	})
	require.ErrorIs(t, err, ErrInvalidListing)
}

func TestNewListingRejectsTradableIndex(t *testing.T) {
	inst, err := NewIndex("SPX")
	require.NoError(t, err)

	_, err = NewListing(ListingParams{
		Instrument: inst,
		Provider:   "Bloomberg",
		Symbol:     "SPX",
		Spec:       validFXSpec(t),
		Tradable:   true,
	})
	require.ErrorIs(t, err, ErrInvalidListing)
}

func TestNewListingAllowsNonTradableContinuousSeries(t *testing.T) {
	inst, err := NewContinuousSeries("ES")
	require.NoError(t, err)

	l, err := NewListing(ListingParams{
		Instrument: inst,
		Provider:   "Trader Research",
		Symbol:     "ES-CONT",
		Spec:       validFXSpec(t),
		Tradable:   false,
	})
	require.NoError(t, err)
	assert.False(t, l.Tradable())
}

func TestNewListingAllowsTradableFuture(t *testing.T) {
	inst := mustFuture(t)

	l, err := NewListing(ListingParams{
		Instrument: inst,
		Provider:   "CME",
		Venue:      "CME",
		Symbol:     "ESZ26",
		Spec:       validFXSpec(t),
		Tradable:   true,
	})
	require.NoError(t, err)
	assert.True(t, l.Tradable())
}
