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

// sharedTestGenerator is package-level and reused across every helper
// call in this package's tests: a Deterministic entropy source combined
// with a Simulated clock that never advances only produces distinct
// values across successive Generate calls on the *same* Generator — a
// fresh Generator per call would replay the identical first value every
// time, silently making every "distinct" test fixture equal.
var sharedTestGenerator = id.NewGenerator(clock.NewSimulated(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))

func testGenerator() *id.Generator {
	return sharedTestGenerator
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

func mustCorrelationID(t *testing.T) id.CorrelationID {
	t.Helper()
	cid, err := id.GenerateCorrelationID(testGenerator())
	require.NoError(t, err)
	return cid
}

func mustIntentID(t *testing.T) id.IntentID {
	t.Helper()
	iid, err := id.GenerateIntentID(testGenerator())
	require.NoError(t, err)
	return iid
}

func mustEurUsdInstrument(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	return inst.ID()
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

func instrumentEquityListing(t *testing.T) (instrument.Listing, error) {
	t.Helper()
	inst, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	return instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "IBKR",
		Venue:      "NASDAQ",
		Symbol:     "AAPL",
		Spec:       spec,
		Tradable:   true,
	})
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

func mustPendingSubmitOrder(t *testing.T) Order {
	t.Helper()
	o, err := NewOrder(Order{
		Request: mustRequest(t),
		Status:  StatusPendingSubmit,
	})
	require.NoError(t, err)
	return o
}

func mustWorkingOrder(t *testing.T, quantity string) Order {
	t.Helper()
	accepted := num.MustParseQuantity(quantity)
	o, err := NewOrder(Order{
		Request:          mustRequest(t),
		BrokerOrderID:    "broker-order-1",
		AcceptedQuantity: &accepted,
		Status:           StatusWorking,
	})
	require.NoError(t, err)
	return o
}

// mustFillFor builds a Fill whose identifying fields match o, so it
// passes ApplyFill's identity checks.
func mustFillFor(t *testing.T, o Order, quantity string) Fill {
	t.Helper()
	f, err := NewFill(Fill{
		FillID:        mustFillID(t),
		OrderID:       o.Request.OrderID,
		BrokerOrderID: o.BrokerOrderID,
		AccountID:     o.Request.AccountID,
		Listing:       o.Request.Listing,
		Side:          o.Request.Side,
		Price:         num.MustParsePrice("1.10000"),
		Quantity:      num.MustParseQuantity(quantity),
	})
	require.NoError(t, err)
	return f
}

// mustCommandMetadata returns Metadata for a new CancelRequest/
// ReplaceRequest, with a fresh non-zero EventID that ApplyCancelRequest/
// ApplyReplaceRequest will record as the order's PendingCommandID.
func mustCommandMetadata(t *testing.T) id.Metadata {
	t.Helper()
	return id.Metadata{EventID: mustEventID(t)}
}

// resultMetadataFor returns Metadata for a CancelResult/ReplaceResult
// that correlates to o's currently outstanding command
// (o.PendingCommandID), as ApplyCancelResult/ApplyReplaceResult require.
func resultMetadataFor(o Order) id.Metadata {
	return id.Metadata{CausationID: o.PendingCommandID}
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
