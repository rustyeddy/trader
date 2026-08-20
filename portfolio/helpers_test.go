package portfolio

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/require"
)

var sharedTestGenerator = id.NewGenerator(clock.NewSimulated(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))

func testGenerator() *id.Generator {
	return sharedTestGenerator
}

func mustAccountID(t *testing.T) id.AccountID {
	t.Helper()
	aid, err := id.GenerateAccountID(testGenerator())
	require.NoError(t, err)
	return aid
}

func mustListing(t *testing.T, base, quote, provider, symbol string) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency(base), num.MustParseCurrency(quote))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency(quote),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   provider,
		Symbol:     symbol,
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func mustPosition(t *testing.T, accountID id.AccountID, listing instrument.Listing) order.Position {
	t.Helper()
	price := num.MustParsePrice("1.10000")
	p, err := order.NewPosition(order.Position{
		AccountID: accountID,
		Listing:   listing,
		Side:      order.Long,
		Quantity:  num.MustParseQuantity("1000"),
		AvgPrice:  &price,
	})
	require.NoError(t, err)
	return p
}

// snapshotParams returns a minimal, valid account.SnapshotParams in
// currency, with no cash balances beyond the account's own currency and
// no open orders.
func snapshotParams(t *testing.T, currency string, equity string) account.SnapshotParams {
	t.Helper()
	cur := num.MustParseCurrency(currency)
	zero := num.MustParseMoney("0", cur)
	return account.SnapshotParams{
		AccountID:       mustAccountID(t),
		Broker:          "OANDA",
		Currency:        cur,
		AsOf:            time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
		CashBalances:    []num.Money{num.MustParseMoney(equity, cur)},
		Equity:          num.MustParseMoney(equity, cur),
		BuyingPower:     num.MustParseMoney(equity, cur),
		MarginUsed:      zero,
		MarginAvailable: num.MustParseMoney(equity, cur),
		RealizedPnL:     zero,
		UnrealizedPnL:   zero,
		Fees:            zero,
		Financing:       zero,
	}
}

func mustSnapshot(t *testing.T, currency, equity string) account.Snapshot {
	t.Helper()
	s, err := account.NewSnapshot(snapshotParams(t, currency, equity))
	require.NoError(t, err)
	return s
}
