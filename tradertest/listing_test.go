package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewListingDefaults(t *testing.T) {
	l, err := tradertest.NewListing(tradertest.ListingParams{})
	require.NoError(t, err)

	assert.Equal(t, "OANDA", l.Provider())
	assert.Equal(t, "EUR_USD", l.Symbol())
	assert.Equal(t, "", l.Venue())
	assert.True(t, l.Tradable())
}

func TestNewListingOverrides(t *testing.T) {
	l, err := tradertest.NewListing(tradertest.ListingParams{
		Base:     "GBP",
		Quote:    "JPY",
		Provider: "IBKR",
		Venue:    "LSE",
		Symbol:   "GBPJPY",
	})
	require.NoError(t, err)

	assert.Equal(t, "IBKR", l.Provider())
	assert.Equal(t, "LSE", l.Venue())
	assert.Equal(t, "GBPJPY", l.Symbol())
}

func TestNewListingRejectsInvalidCurrency(t *testing.T) {
	_, err := tradertest.NewListing(tradertest.ListingParams{Base: "not-a-currency"})
	require.Error(t, err)
}

func TestMustNewListingPanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		tradertest.MustNewListing(tradertest.ListingParams{Base: "not-a-currency"})
	})
}
