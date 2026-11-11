package broker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func validSimListingFlags() simListingFlags {
	return simListingFlags{
		symbol:            "EURUSD",
		tickSize:          "0.00001",
		quantityIncrement: "1",
		multiplier:        "1",
	}
}

func TestBuildSimListing(t *testing.T) {
	t.Run("valid symbol and spec builds a tradable listing", func(t *testing.T) {
		listing, err := buildSimListing(validSimListingFlags(), "sim")
		require.NoError(t, err)
		require.Equal(t, "EURUSD", listing.Symbol())
		require.True(t, listing.Tradable())
	})

	t.Run("lowercase symbol is normalized", func(t *testing.T) {
		flags := validSimListingFlags()
		flags.symbol = "eurusd"
		listing, err := buildSimListing(flags, "sim")
		require.NoError(t, err)
		require.Equal(t, "EURUSD", listing.Symbol())
	})

	t.Run("wrong-length symbol is rejected", func(t *testing.T) {
		flags := validSimListingFlags()
		flags.symbol = "EUR"
		_, err := buildSimListing(flags, "sim")
		require.ErrorContains(t, err, "6-letter FX pair symbol")
	})

	t.Run("unknown currency is rejected", func(t *testing.T) {
		flags := validSimListingFlags()
		flags.symbol = "12EUSD"
		_, err := buildSimListing(flags, "sim")
		require.Error(t, err)
	})

	t.Run("invalid tick size is rejected", func(t *testing.T) {
		flags := validSimListingFlags()
		flags.tickSize = "not-a-number"
		_, err := buildSimListing(flags, "sim")
		require.ErrorContains(t, err, "--tick-size")
	})

	t.Run("invalid quantity increment is rejected", func(t *testing.T) {
		flags := validSimListingFlags()
		flags.quantityIncrement = "not-a-number"
		_, err := buildSimListing(flags, "sim")
		require.ErrorContains(t, err, "--quantity-increment")
	})

	t.Run("invalid multiplier is rejected", func(t *testing.T) {
		flags := validSimListingFlags()
		flags.multiplier = "not-a-number"
		_, err := buildSimListing(flags, "sim")
		require.ErrorContains(t, err, "--multiplier")
	})
}
