package external_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
)

var testIDs = id.NewGenerator(clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))

func newIntentFactory(t *testing.T, source id.Source) strategy.IntentFactory {
	t.Helper()
	c := clock.NewSimulated(time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC))
	return strategy.NewIntentFactory(c, testIDs, source)
}

func eurUSD(t *testing.T) instrument.ID {
	t.Helper()
	base := num.MustParseCurrency("EUR")
	quote := num.MustParseCurrency("USD")
	return instrument.CurrencyPairID(base, quote)
}

func eurUSDListing(t *testing.T) instrument.Listing {
	t.Helper()
	base := num.MustParseCurrency("EUR")
	quote := num.MustParseCurrency("USD")
	inst, err := instrument.NewCurrencyPair(base, quote)
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		quote,
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func gbpUSDListing(t *testing.T) instrument.Listing {
	t.Helper()
	base := num.MustParseCurrency("GBP")
	quote := num.MustParseCurrency("USD")
	inst, err := instrument.NewCurrencyPair(base, quote)
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		quote,
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "GBP_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func usdJPYListing(t *testing.T) instrument.Listing {
	t.Helper()
	base := num.MustParseCurrency("USD")
	quote := num.MustParseCurrency("JPY")
	inst, err := instrument.NewCurrencyPair(base, quote)
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		quote,
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "USD_JPY",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func mustAccountID(t *testing.T) id.AccountID {
	t.Helper()
	v, err := id.GenerateAccountID(testIDs)
	require.NoError(t, err)
	return v
}

// testSnapshot builds a minimal, valid account.Snapshot with one long
// EUR/USD position, for tests that only need ToWireAccountSnapshot's
// own inputs.
func testSnapshot(t *testing.T) account.Snapshot {
	t.Helper()
	acctID := mustAccountID(t)
	avg := num.MustParsePrice("1.1000")
	pos, err := order.NewPosition(order.Position{
		AccountID: acctID,
		Listing:   eurUSDListing(t),
		Side:      order.Long,
		Quantity:  num.MustParseQuantity("1000"),
		AvgPrice:  &avg,
	})
	require.NoError(t, err)

	usd := num.MustParseCurrency("USD")
	zero := num.MustParseMoney("0", usd)
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       acctID,
		Broker:          "sim",
		Currency:        usd,
		AsOf:            time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC),
		Positions:       []order.Position{pos},
		Equity:          zero,
		BuyingPower:     zero,
		MarginUsed:      zero,
		MarginAvailable: zero,
		RealizedPnL:     zero,
		UnrealizedPnL:   zero,
		Fees:            zero,
		Financing:       zero,
	})
	require.NoError(t, err)
	return snap
}

// testFlatSnapshot builds a minimal, valid account.Snapshot with no
// open positions.
func testFlatSnapshot(t *testing.T) account.Snapshot {
	t.Helper()
	usd := num.MustParseCurrency("USD")
	zero := num.MustParseMoney("0", usd)
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       mustAccountID(t),
		Broker:          "sim",
		Currency:        usd,
		AsOf:            time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC),
		Equity:          zero,
		BuyingPower:     zero,
		MarginUsed:      zero,
		MarginAvailable: zero,
		RealizedPnL:     zero,
		UnrealizedPnL:   zero,
		Fees:            zero,
		Financing:       zero,
	})
	require.NoError(t, err)
	return snap
}
