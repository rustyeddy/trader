package alpaca

import (
	"testing"
	"time"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testListing builds a whole-share-only equity Listing registered under
// provider "alpaca", matching this adapter's Broker.Name() and the same
// provider tag the equity Market Data provider (issue #297) registers
// Listings under.
func testListing(t *testing.T, ticker string) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewEquity("NASDAQ", ticker)
	require.NoError(t, err)
	usd := num.MustParseCurrency("USD")
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		usd,
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "alpaca",
		Symbol:     ticker,
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func testResolver(t *testing.T, listings ...instrument.Listing) instrument.Resolver {
	t.Helper()
	r := instrument.NewMemoryResolver()
	for _, l := range listings {
		require.NoError(t, r.Register(l))
	}
	return r
}

func TestSideTranslation(t *testing.T) {
	s, err := sideToWire(order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "buy", s)
	assert.Equal(t, order.Buy, sideFromWire("buy"))
	assert.Equal(t, order.Sell, sideFromWire("sell"))
	assert.Zero(t, sideFromWire("bogus"))
}

func TestTypeTranslation(t *testing.T) {
	s, err := typeToWire(order.Market)
	require.NoError(t, err)
	assert.Equal(t, "market", s)

	_, err = typeToWire(order.Limit)
	assert.ErrorIs(t, err, brokerpkg.ErrUnsupported)

	assert.Equal(t, order.Limit, typeFromWire("limit"))
	assert.Equal(t, order.Stop, typeFromWire("stop"))
	assert.Equal(t, order.StopLimit, typeFromWire("stop_limit"))
	assert.Zero(t, typeFromWire("bogus"))
}

func TestTimeInForceTranslation(t *testing.T) {
	for _, tif := range []order.TimeInForce{order.GTC, order.DAY, order.IOC, order.FOK} {
		s, err := tifToWire(tif)
		require.NoError(t, err)
		assert.Equal(t, tif, tifFromWire(s))
	}
}

func TestStatusFromWire(t *testing.T) {
	cases := map[string]order.Status{
		"new":              order.StatusWorking,
		"accepted":         order.StatusWorking,
		"pending_new":      order.StatusWorking,
		"partially_filled": order.StatusPartiallyFilled,
		"filled":           order.StatusFilled,
		"pending_cancel":   order.StatusPendingCancel,
		"canceled":         order.StatusCanceled,
		"pending_replace":  order.StatusPendingReplace,
		"replaced":         order.StatusPendingReplace,
		"rejected":         order.StatusRejected,
		"expired":          order.StatusExpired,
		"done_for_day":     order.StatusExpired,
		"something_new":    order.StatusUnknown,
	}
	for wire, want := range cases {
		assert.Equal(t, want, statusFromWire(wire), "wire status %q", wire)
	}
}

func TestRequestToWireSubmit_MarketOnly(t *testing.T) {
	listing := testListing(t, "AAPL")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	req, err := order.NewRequest(order.Proposal{
		Listing:     listing,
		AccountID:   id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.DAY,
		Quantity:    num.MustParseQuantity("10"),
	}, orderID)
	require.NoError(t, err)

	wire, err := requestToWireSubmit(req)
	require.NoError(t, err)
	assert.Equal(t, "AAPL", wire.Symbol)
	assert.Equal(t, "10", wire.Qty)
	assert.Equal(t, "buy", wire.Side)
	assert.Equal(t, "market", wire.Type)
	assert.Equal(t, "day", wire.TimeInForce)
	assert.Equal(t, orderID.String(), wire.ClientOrderID)
}

func TestRequestToWireSubmit_RejectsNonMarket(t *testing.T) {
	listing := testListing(t, "AAPL")
	price := num.MustParsePrice("150.00")
	req, err := order.NewRequest(order.Proposal{
		Listing:     listing,
		AccountID:   id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Side:        order.Buy,
		Type:        order.Limit,
		TimeInForce: order.DAY,
		Quantity:    num.MustParseQuantity("10"),
		LimitPrice:  &price,
	}, id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV"))
	require.NoError(t, err)

	_, err = requestToWireSubmit(req)
	require.Error(t, err)
}

func TestWireOrderToOrder_Working(t *testing.T) {
	listing := testListing(t, "AAPL")
	resolver := testResolver(t, listing)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")

	wo := wireOrder{
		ID: "alpaca-1", ClientOrderID: orderID.String(), Symbol: "AAPL",
		Qty: "10", FilledQty: "0", Type: "market", Side: "buy",
		TimeInForce: "day", Status: "new",
	}
	o, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.NoError(t, err)
	assert.Equal(t, order.StatusWorking, o.Status)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(num.MustParseQuantity("10")))
	assert.True(t, o.FilledQuantity.IsZero())
	assert.Equal(t, "alpaca-1", o.BrokerOrderID)
	assert.Equal(t, orderID, o.Request.OrderID)
}

func TestWireOrderToOrder_Filled(t *testing.T) {
	listing := testListing(t, "AAPL")
	resolver := testResolver(t, listing)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	avgPrice := "150.25"

	wo := wireOrder{
		ID: "alpaca-1", ClientOrderID: orderID.String(), Symbol: "AAPL",
		Qty: "10", FilledQty: "10", FilledAvgPrice: &avgPrice, Type: "market",
		Side: "buy", TimeInForce: "day", Status: "filled",
	}
	o, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, o.Status)
	assert.True(t, o.FilledQuantity.Equal(num.MustParseQuantity("10")))
	require.NotNil(t, o.AvgFillPrice)
	assert.True(t, o.AvgFillPrice.Equal(num.MustParsePrice("150.25")))
}

func TestWireOrderToOrder_Rejected(t *testing.T) {
	listing := testListing(t, "AAPL")
	resolver := testResolver(t, listing)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")

	wo := wireOrder{
		ID: "alpaca-1", ClientOrderID: orderID.String(), Symbol: "AAPL",
		Qty: "10", FilledQty: "0", Type: "market", Side: "buy",
		TimeInForce: "day", Status: "rejected",
	}
	o, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.NoError(t, err)
	assert.Equal(t, order.StatusRejected, o.Status)
	assert.Nil(t, o.AcceptedQuantity)
	require.NotNil(t, o.Rejection)
}

func TestWireOrderToOrder_UnresolvedSymbolFails(t *testing.T) {
	resolver := testResolver(t) // empty
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")

	wo := wireOrder{
		ID: "alpaca-1", ClientOrderID: orderID.String(), Symbol: "UNKNOWN",
		Qty: "10", FilledQty: "0", Type: "market", Side: "buy",
		TimeInForce: "day", Status: "new",
	}
	_, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.Error(t, err)
}

func TestWirePositionToPosition(t *testing.T) {
	listing := testListing(t, "AAPL")
	resolver := testResolver(t, listing)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")

	pos, err := wirePositionToPosition(wirePosition{
		Symbol: "AAPL", Side: "long", Qty: "10", AvgEntryPrice: "150.00", UnrealizedPL: "5.00",
	}, resolver, "alpaca", accountID)
	require.NoError(t, err)
	assert.Equal(t, order.Long, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("10")))
	require.NotNil(t, pos.AvgPrice)
	assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("150.00")))
}

func TestWirePositionToPosition_InvalidSide(t *testing.T) {
	listing := testListing(t, "AAPL")
	resolver := testResolver(t, listing)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")

	_, err := wirePositionToPosition(wirePosition{
		Symbol: "AAPL", Side: "bogus", Qty: "10", AvgEntryPrice: "150.00",
	}, resolver, "alpaca", accountID)
	require.Error(t, err)
}

func TestWireAccountToSnapshotParams(t *testing.T) {
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	unrealized := num.MustParseMoney("5.00", num.MustParseCurrency("USD"))
	params, err := wireAccountToSnapshotParams(wireAccount{
		Currency: "USD", Cash: "100000", Equity: "100005",
		BuyingPower: "200000", InitialMargin: "0", MaintenanceMargin: "0",
	}, nil, nil, unrealized, accountID, "alpaca", time.Now())
	require.NoError(t, err)
	assert.Equal(t, accountID, params.AccountID)
	assert.Equal(t, "alpaca", params.Broker)
	assert.True(t, params.UnrealizedPnL.Equal(unrealized))
	assert.True(t, params.RealizedPnL.IsZero())
}
