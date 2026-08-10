package instrument

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewListing(t *testing.T) {
	instID := CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	spec := validFXSpec(t)

	l, err := NewListing(ListingParams{
		InstrumentID: instID,
		Venue:        "OANDA",
		Symbol:       "EUR_USD",
		Spec:         spec,
		Tradable:     true,
	})
	require.NoError(t, err)

	assert.True(t, l.InstrumentID().Equal(instID))
	assert.Equal(t, "OANDA", l.Venue())
	assert.Equal(t, "EUR_USD", l.Symbol())
	assert.True(t, l.Tradable())
	assert.Equal(t, spec, l.Spec())
}

func TestNewListingPreservesSymbolCaseAndTrimsWhitespace(t *testing.T) {
	instID := IndexID("SPX")
	spec := validFXSpec(t)

	l, err := NewListing(ListingParams{
		InstrumentID: instID,
		Venue:        "  NASDAQ  ",
		Symbol:       "  spx.us  ",
		Spec:         spec,
	})
	require.NoError(t, err)

	assert.Equal(t, "NASDAQ", l.Venue())
	assert.Equal(t, "spx.us", l.Symbol(), "provider symbol text is display-only and never normalized")
}

func TestNewListingRejectsZeroInstrumentID(t *testing.T) {
	_, err := NewListing(ListingParams{
		Venue:  "OANDA",
		Symbol: "EUR_USD",
		Spec:   validFXSpec(t),
	})
	require.ErrorIs(t, err, ErrInvalidListing)
}

func TestNewListingRejectsEmptyVenueOrSymbol(t *testing.T) {
	instID := IndexID("SPX")
	spec := validFXSpec(t)

	_, err := NewListing(ListingParams{InstrumentID: instID, Venue: "", Symbol: "SPX", Spec: spec})
	require.ErrorIs(t, err, ErrInvalidListing)

	_, err = NewListing(ListingParams{InstrumentID: instID, Venue: "NASDAQ", Symbol: "", Spec: spec})
	require.ErrorIs(t, err, ErrInvalidListing)
}

func TestNewListingRejectsUnconstructedSpec(t *testing.T) {
	instID := IndexID("SPX")

	_, err := NewListing(ListingParams{
		InstrumentID: instID,
		Venue:        "NASDAQ",
		Symbol:       "SPX",
		Spec:         Spec{},
	})
	require.ErrorIs(t, err, ErrInvalidListing)
}

// TestContinuousSeriesListingIsNeverTradableByConvention documents that
// tradability is decided by the caller, driven by Instrument.Kind, not
// enforced structurally by Listing — see the package doc comment for why
// that distinction belongs to Kind rather than a Listing-level flag
// combination.
func TestContinuousSeriesListingIsNeverTradableByConvention(t *testing.T) {
	inst, err := NewContinuousSeries("ES")
	require.NoError(t, err)

	l, err := NewListing(ListingParams{
		InstrumentID: inst.ID(),
		Venue:        "Trader Research",
		Symbol:       "ES-CONT",
		Spec:         validFXSpec(t),
		Tradable:     false,
	})
	require.NoError(t, err)
	assert.False(t, l.Tradable())
}
