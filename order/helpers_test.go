package order

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/require"
)

func testGenerator() *id.Generator {
	return id.NewGenerator(clock.NewSimulated(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))
}

func mustOrderID(t *testing.T) id.OrderID {
	t.Helper()
	oid, err := id.GenerateOrderID(testGenerator())
	require.NoError(t, err)
	return oid
}

func mustFillID(t *testing.T) id.FillID {
	t.Helper()
	fid, err := id.GenerateFillID(testGenerator())
	require.NoError(t, err)
	return fid
}

func mustAccountID(t *testing.T) id.AccountID {
	t.Helper()
	aid, err := id.GenerateAccountID(testGenerator())
	require.NoError(t, err)
	return aid
}

func mustEurUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "OANDA",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func mustProposal(t *testing.T) Proposal {
	t.Helper()
	p, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Market,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)
	return p
}

func mustEventID(t *testing.T) id.EventID {
	t.Helper()
	eid, err := id.GenerateEventID(testGenerator())
	require.NoError(t, err)
	return eid
}

func mustRequest(t *testing.T) Request {
	t.Helper()
	r, err := NewRequest(mustProposal(t), mustOrderID(t))
	require.NoError(t, err)
	return r
}

func price(t *testing.T, s string) *num.Price {
	t.Helper()
	p := num.MustParsePrice(s)
	return &p
}

func qty(t *testing.T, s string) *num.Quantity {
	t.Helper()
	q := num.MustParseQuantity(s)
	return &q
}
