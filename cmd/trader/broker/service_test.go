package broker

import (
	"testing"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoPriceSource proves noPriceSource's own contract directly: it
// reports a real ModelInfo (so a formatter that inspects it never sees
// a zero value) but always refuses to price anything, since the
// accounts/snapshot commands it backs never call Submit and so should
// never actually reach FillPriceSource.Price in normal operation.
func TestNoPriceSource(t *testing.T) {
	src := noPriceSource{}
	assert.Equal(t, "none", src.Info().Name)

	_, err := src.Price(mustSimListing(t), order.Buy)
	require.ErrorContains(t, err, "no fill price source configured")
}

// TestCliPriceSource proves cliPriceSource only prices the one symbol
// it was configured for -- a submit against a different listing (which
// should never happen in practice, since buildSimListing and
// cliPriceSource are built from the same --symbol flag, but the type
// itself must still fail closed rather than silently pricing the wrong
// instrument) is rejected rather than silently using the configured
// price.
func TestCliPriceSource(t *testing.T) {
	src := cliPriceSource{symbol: "EURUSD", price: mustPrice(t, "1.10000")}
	assert.Contains(t, src.Info().Config, "EURUSD")

	price, err := src.Price(mustSimListing(t), order.Buy)
	require.NoError(t, err)
	assert.True(t, price.Equal(mustPrice(t, "1.10000")))

	other := mustSimListingWithSymbol(t, "GBPUSD")
	_, err = src.Price(other, order.Sell)
	require.ErrorContains(t, err, "no price configured for GBPUSD")
}

func TestResolveSubmitPriceSource(t *testing.T) {
	t.Run("market order requires a price", func(t *testing.T) {
		_, err := resolveSubmitPriceSource(order.Market, "EURUSD", "")
		require.ErrorContains(t, err, "--price is required")
	})

	t.Run("market order with a price builds a cliPriceSource", func(t *testing.T) {
		src, err := resolveSubmitPriceSource(order.Market, "EURUSD", "1.10000")
		require.NoError(t, err)
		require.IsType(t, cliPriceSource{}, src)
	})

	t.Run("market order with an invalid price is rejected", func(t *testing.T) {
		_, err := resolveSubmitPriceSource(order.Market, "EURUSD", "not-a-number")
		require.ErrorContains(t, err, "--price")
	})

	t.Run("non-market order needs no price and uses noPriceSource", func(t *testing.T) {
		src, err := resolveSubmitPriceSource(order.Limit, "EURUSD", "")
		require.NoError(t, err)
		require.IsType(t, noPriceSource{}, src)
	})
}

func mustSimListing(t *testing.T) instrument.Listing {
	t.Helper()
	return mustSimListingWithSymbol(t, "EURUSD")
}

func mustSimListingWithSymbol(t *testing.T, symbol string) instrument.Listing {
	t.Helper()
	listing, err := buildSimListing(simListingFlags{
		symbol:            symbol,
		tickSize:          "0.00001",
		quantityIncrement: "1",
		multiplier:        "1",
	}, "sim")
	require.NoError(t, err)
	return listing
}

func mustPrice(t *testing.T, s string) num.Price {
	t.Helper()
	p, err := num.ParsePrice(s)
	require.NoError(t, err)
	return p
}
