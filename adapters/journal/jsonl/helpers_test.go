package jsonl_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var testIDs = id.NewGenerator(clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))

func mustRunID(t *testing.T) id.RunID {
	t.Helper()
	v, err := id.GenerateRunID(testIDs)
	require.NoError(t, err)
	return v
}

func mustEventID(t *testing.T) id.EventID {
	t.Helper()
	v, err := id.GenerateEventID(testIDs)
	require.NoError(t, err)
	return v
}

func mustCorrelationID(t *testing.T) id.CorrelationID {
	t.Helper()
	v, err := id.GenerateCorrelationID(testIDs)
	require.NoError(t, err)
	return v
}

func mustIntentID(t *testing.T) id.IntentID {
	t.Helper()
	v, err := id.GenerateIntentID(testIDs)
	require.NoError(t, err)
	return v
}

func mustAccountID(t *testing.T) id.AccountID {
	t.Helper()
	v, err := id.GenerateAccountID(testIDs)
	require.NoError(t, err)
	return v
}

func mustOrderID(t *testing.T) id.OrderID {
	t.Helper()
	v, err := id.GenerateOrderID(testIDs)
	require.NoError(t, err)
	return v
}

func mustFillID(t *testing.T) id.FillID {
	t.Helper()
	v, err := id.GenerateFillID(testIDs)
	require.NoError(t, err)
	return v
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
		Provider:   "sim",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func mustIntent(t *testing.T) order.Intent {
	t.Helper()
	in, err := order.NewIntent(order.Intent{
		IntentID:   mustIntentID(t),
		Kind:       order.IntentEnter,
		Instrument: mustEurUsdListing(t).InstrumentID(),
		Side:       order.Buy,
		Metadata:   id.Metadata{EventID: mustEventID(t), CorrelationID: mustCorrelationID(t)},
	})
	require.NoError(t, err)
	return in
}

func mustProposal(t *testing.T) order.Proposal {
	t.Helper()
	p, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t), CorrelationID: mustCorrelationID(t)},
	})
	require.NoError(t, err)
	return p
}

func mustRequest(t *testing.T) order.Request {
	t.Helper()
	r, err := order.NewRequest(mustProposal(t), mustOrderID(t))
	require.NoError(t, err)
	return r
}

func mustWorkingOrder(t *testing.T) order.Order {
	t.Helper()
	return mustWorkingOrderFor(t, mustAccountID(t))
}

func mustWorkingOrderFor(t *testing.T, accountID id.AccountID) order.Order {
	t.Helper()
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t), CorrelationID: mustCorrelationID(t)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t))
	require.NoError(t, err)

	accepted := num.MustParseQuantity("1000")
	o, err := order.NewOrder(order.Order{
		Request:          req,
		BrokerOrderID:    "sim-1",
		AcceptedQuantity: &accepted,
		Status:           order.StatusWorking,
		UpdatedAt:        time.Now(),
	})
	require.NoError(t, err)
	return o
}

func mustFill(t *testing.T) order.Fill {
	t.Helper()
	commission := num.MustParseMoney("1.00", num.MustParseCurrency("USD"))
	f, err := order.NewFill(order.Fill{
		FillID:     mustFillID(t),
		OrderID:    mustOrderID(t),
		AccountID:  mustAccountID(t),
		Listing:    mustEurUsdListing(t),
		Side:       order.Buy,
		Price:      num.MustParsePrice("1.10000"),
		Quantity:   num.MustParseQuantity("1000"),
		Commission: &commission,
		Timestamp:  time.Now(),
		Metadata:   id.Metadata{EventID: mustEventID(t), CorrelationID: mustCorrelationID(t)},
	})
	require.NoError(t, err)
	return f
}

func mustSnapshot(t *testing.T) account.Snapshot {
	t.Helper()
	usd := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	avgPrice := num.MustParsePrice("1.10000")
	position, err := order.NewPosition(order.Position{
		AccountID: accountID,
		Listing:   mustEurUsdListing(t),
		Side:      order.Long,
		Quantity:  num.MustParseQuantity("1000"),
		AvgPrice:  &avgPrice,
	})
	require.NoError(t, err)

	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       accountID,
		Broker:          "sim",
		Currency:        usd,
		AsOf:            time.Now(),
		CashBalances:    []num.Money{num.MustParseMoney("10000", usd)},
		Equity:          num.MustParseMoney("10000", usd),
		BuyingPower:     num.MustParseMoney("10000", usd),
		MarginUsed:      num.MustParseMoney("0", usd),
		MarginAvailable: num.MustParseMoney("10000", usd),
		RealizedPnL:     num.MustParseMoney("0", usd),
		UnrealizedPnL:   num.MustParseMoney("0", usd),
		Fees:            num.MustParseMoney("0", usd),
		Financing:       num.MustParseMoney("0", usd),
		Positions:       []order.Position{position},
		OpenOrders:      []order.Order{mustWorkingOrderFor(t, accountID)},
	})
	require.NoError(t, err)
	return snap
}

func mustRejectedOrder(t *testing.T) order.Order {
	t.Helper()
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t), CorrelationID: mustCorrelationID(t)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t))
	require.NoError(t, err)
	o, err := order.NewOrder(order.Order{
		Request: req,
		Status:  order.StatusRejected,
		Rejection: &order.Rejection{
			Reason:     order.ReasonInsufficientMargin,
			Detail:     "not enough margin",
			BrokerCode: "SIM-REJ-1",
		},
	})
	require.NoError(t, err)
	return o
}

func mustStatus() broker.Status {
	return broker.Status{State: broker.AccountStatusDegraded, BrokerCode: "SIM-1", Message: "reduced confidence"}
}

func mustTrade(t *testing.T) order.Trade {
	t.Helper()
	usd := num.MustParseCurrency("USD")
	tr, err := order.NewTrade(order.Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         order.Long,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		OpenedAt:     time.Now(),
		RealizedPnL:  num.MustParseMoney("0", usd),
		Costs:        num.MustParseMoney("0", usd),
	})
	require.NoError(t, err)
	return tr
}
