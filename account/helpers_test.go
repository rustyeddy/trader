package account

import (
	"testing"
	"time"

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

func mustOrderID(t *testing.T) id.OrderID {
	t.Helper()
	oid, err := id.GenerateOrderID(testGenerator())
	require.NoError(t, err)
	return oid
}

func mustEventID(t *testing.T) id.EventID {
	t.Helper()
	eid, err := id.GenerateEventID(testGenerator())
	require.NoError(t, err)
	return eid
}

func mustEurUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	return mustListing(t, "EUR", "USD", "OANDA", "EUR_USD")
}

func mustGbpUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	return mustListing(t, "GBP", "USD", "OANDA", "GBP_USD")
}

// mustListing builds a currency-pair Listing under the given provider,
// so tests can exercise cross-provider mismatches against a Snapshot's
// Broker.
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

func mustWorkingOrder(t *testing.T, accountID id.AccountID, listing instrument.Listing) order.Order {
	t.Helper()
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)
	request, err := order.NewRequest(proposal, mustOrderID(t))
	require.NoError(t, err)
	accepted := num.MustParseQuantity("1000")
	o, err := order.NewOrder(order.Order{
		Request:          request,
		BrokerOrderID:    "broker-order-1",
		AcceptedQuantity: &accepted,
		Status:           order.StatusWorking,
	})
	require.NoError(t, err)
	return o
}

// mustPartiallyFilledLimitOrder builds a StopLimit order (so both
// LimitPrice and StopPrice are populated) that has received one partial
// fill: it exercises every pointer/slice field cloneOrder must copy
// (Request.LimitPrice, Request.StopPrice, AcceptedLimitPrice,
// AcceptedStopPrice, AppliedFillIDs, AppliedBrokerFillIDs) except
// Rejection and AvgFillPrice, which only ever coexist with StatusRejected
// or a fill respectively — see mustRejectedOrder for Rejection.
func mustPartiallyFilledLimitOrder(t *testing.T, accountID id.AccountID, listing instrument.Listing) order.Order {
	t.Helper()
	limitPrice := num.MustParsePrice("1.09000")
	stopPrice := num.MustParsePrice("1.08000")
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.StopLimit,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  &limitPrice,
		StopPrice:   &stopPrice,
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)
	request, err := order.NewRequest(proposal, mustOrderID(t))
	require.NoError(t, err)

	accepted := num.MustParseQuantity("1000")
	acceptedLimit := num.MustParsePrice("1.09000")
	acceptedStop := num.MustParsePrice("1.08000")
	o, err := order.NewOrder(order.Order{
		Request:            request,
		BrokerOrderID:      "broker-order-2",
		AcceptedQuantity:   &accepted,
		AcceptedLimitPrice: &acceptedLimit,
		AcceptedStopPrice:  &acceptedStop,
		Status:             order.StatusWorking,
	})
	require.NoError(t, err)

	fillID, err := id.GenerateFillID(testGenerator())
	require.NoError(t, err)
	fill, err := order.NewFill(order.Fill{
		FillID:        fillID,
		OrderID:       o.Request.OrderID,
		BrokerOrderID: o.BrokerOrderID,
		BrokerFillID:  "broker-fill-1",
		AccountID:     o.Request.AccountID,
		Listing:       o.Request.Listing,
		Side:          o.Request.Side,
		Price:         num.MustParsePrice("1.09000"),
		Quantity:      num.MustParseQuantity("400"),
	})
	require.NoError(t, err)
	partial, err := order.ApplyFill(o, fill)
	require.NoError(t, err)
	return partial
}

func mustFilledOrder(t *testing.T, accountID id.AccountID, listing instrument.Listing) order.Order {
	t.Helper()
	o := mustWorkingOrder(t, accountID, listing)
	fillID, err := id.GenerateFillID(testGenerator())
	require.NoError(t, err)
	fill, err := order.NewFill(order.Fill{
		FillID:        fillID,
		OrderID:       o.Request.OrderID,
		BrokerOrderID: o.BrokerOrderID,
		AccountID:     o.Request.AccountID,
		Listing:       o.Request.Listing,
		Side:          o.Request.Side,
		Price:         num.MustParsePrice("1.10000"),
		Quantity:      *o.AcceptedQuantity,
	})
	require.NoError(t, err)
	filled, err := order.ApplyFill(o, fill)
	require.NoError(t, err)
	return filled
}

func usd(s string) num.Money {
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
}

func eur(s string) num.Money {
	return num.MustParseMoney(s, num.MustParseCurrency("EUR"))
}
