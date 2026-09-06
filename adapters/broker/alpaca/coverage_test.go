package alpaca

import (
	"context"
	"strings"
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

// This file rounds out coverage for edge/error/failure-mode branches
// not naturally exercised by the primary behavior tests elsewhere in
// this package, per AGENTS.md's "corner, edge and failure mode"
// testing requirement.

func TestDeps_ValidateEachMissingField(t *testing.T) {
	resolver := testResolver(t)
	full := testDeps(t, resolver)

	missingClock := full
	missingClock.Clock = nil
	require.Error(t, missingClock.validate())

	missingIDs := full
	missingIDs.IDs = nil
	require.Error(t, missingIDs.validate())

	missingResolver := full
	missingResolver.Resolver = nil
	require.Error(t, missingResolver.validate())

	require.NoError(t, full.validate())
}

func TestDeps_PollIntervalDefault(t *testing.T) {
	var d Deps
	assert.Equal(t, defaultPollInterval, d.pollInterval())

	d.PollInterval = 5
	assert.Equal(t, d.PollInterval, d.pollInterval())
}

func TestClassifyStatus_EveryClass(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{400, ErrBadRequest},
		{404, ErrBadRequest},
		{401, ErrUnauthorized},
		{403, ErrUnauthorized},
		{422, ErrOrderRejected},
		{429, ErrRateLimited},
		{500, ErrProviderUnavailable},
		{503, ErrProviderUnavailable},
		{418, ErrUnexpectedStatus},
	}
	for _, c := range cases {
		err := classifyStatus(c.status, strings.NewReader("body"))
		assert.ErrorIs(t, err, c.want, "status %d", c.status)
	}
}

func TestClassifyOrderStatus_NotFoundDistinctFromBadRequest(t *testing.T) {
	err := classifyOrderStatus(404, strings.NewReader(""))
	assert.ErrorIs(t, err, ErrOrderNotFound)

	err = classifyOrderStatus(400, strings.NewReader(""))
	assert.ErrorIs(t, err, ErrBadRequest)

	err = classifyOrderStatus(500, strings.NewReader(""))
	assert.ErrorIs(t, err, ErrProviderUnavailable)
}

func TestSideToWire_InvalidSide(t *testing.T) {
	_, err := sideToWire(order.Side(99))
	require.Error(t, err)
}

func TestTifToWire_Invalid(t *testing.T) {
	_, err := tifToWire(order.TimeInForce(99))
	require.Error(t, err)
	assert.Zero(t, tifFromWire("bogus"))
}

func TestDecodeCursor_Malformed(t *testing.T) {
	assert.Equal(t, uint64(0), decodeCursor("not-a-number"))
	assert.Equal(t, uint64(0), decodeCursor(""))
	assert.Equal(t, uint64(42), decodeCursor(encodeCursor(42)))
}

func TestRequestToWireSubmit_InvalidSide(t *testing.T) {
	listing := testListing(t, "AAPL")
	proposal := order.Proposal{
		Listing: listing, AccountID: id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Side: order.Buy, Type: order.Market, TimeInForce: order.DAY, Quantity: num.MustParseQuantity("1"),
	}
	req, err := order.NewRequest(proposal, id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV"))
	require.NoError(t, err)
	// Force an invalid Side after construction to exercise
	// requestToWireSubmit's own translation failure path directly
	// (order.NewRequest would itself reject an invalid Side, so this
	// bypasses that constructor deliberately).
	req.Side = order.Side(99)
	_, err = requestToWireSubmit(req)
	require.Error(t, err)
}

func TestWireOrderToOrder_InvalidClientOrderID(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	wo := wireOrder{ID: "a1", ClientOrderID: "not-an-order-id", Symbol: "AAPL", Qty: "1", Type: "market", Side: "buy", TimeInForce: "day", Status: "new"}
	_, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.Error(t, err)
}

func TestWireOrderToOrder_InvalidQty(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	wo := wireOrder{ID: "a1", ClientOrderID: orderID.String(), Symbol: "AAPL", Qty: "not-a-number", Type: "market", Side: "buy", TimeInForce: "day", Status: "new"}
	_, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.Error(t, err)
}

func TestWireOrderToOrder_InvalidLimitPrice(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	bad := "not-a-price"
	wo := wireOrder{ID: "a1", ClientOrderID: orderID.String(), Symbol: "AAPL", Qty: "1", Type: "limit", Side: "buy", TimeInForce: "day", Status: "new", LimitPrice: &bad}
	_, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.Error(t, err)
}

func TestWireOrderToOrder_InvalidStopPrice(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	bad := "not-a-price"
	wo := wireOrder{ID: "a1", ClientOrderID: orderID.String(), Symbol: "AAPL", Qty: "1", Type: "stop", Side: "buy", TimeInForce: "day", Status: "new", StopPrice: &bad}
	_, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.Error(t, err)
}

func TestWireOrderToOrder_InvalidFilledQty(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	wo := wireOrder{ID: "a1", ClientOrderID: orderID.String(), Symbol: "AAPL", Qty: "1", FilledQty: "bogus", Type: "market", Side: "buy", TimeInForce: "day", Status: "new"}
	_, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.Error(t, err)
}

func TestWireOrderToOrder_InvalidFilledAvgPrice(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	bad := "bogus"
	wo := wireOrder{ID: "a1", ClientOrderID: orderID.String(), Symbol: "AAPL", Qty: "1", FilledQty: "1", FilledAvgPrice: &bad, Type: "market", Side: "buy", TimeInForce: "day", Status: "filled"}
	_, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.Error(t, err)
}

func TestWirePositionToPosition_InvalidQty(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	_, err := wirePositionToPosition(wirePosition{Symbol: "AAPL", Side: "long", Qty: "bogus", AvgEntryPrice: "1"}, resolver, "alpaca", accountID)
	require.Error(t, err)
}

func TestWirePositionToPosition_InvalidAvgPrice(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	_, err := wirePositionToPosition(wirePosition{Symbol: "AAPL", Side: "long", Qty: "1", AvgEntryPrice: "bogus"}, resolver, "alpaca", accountID)
	require.Error(t, err)
}

func TestWirePositionToPosition_UnresolvedSymbol(t *testing.T) {
	resolver := testResolver(t)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	_, err := wirePositionToPosition(wirePosition{Symbol: "UNKNOWN", Side: "long", Qty: "1", AvgEntryPrice: "1"}, resolver, "alpaca", accountID)
	require.Error(t, err)
}

func TestWireAccountToSnapshotParams_ErrorPaths(t *testing.T) {
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	usd := num.MustParseCurrency("USD")
	zero := num.MustParseMoney("0", usd)
	baseline := wireAccount{Currency: "USD", Cash: "1", Equity: "1", BuyingPower: "1", InitialMargin: "0"}

	bad := baseline
	bad.Currency = "???"
	_, err := wireAccountToSnapshotParams(bad, nil, nil, zero, accountID, "alpaca", time.Now())
	require.Error(t, err)

	bad = baseline
	bad.Cash = "bogus"
	_, err = wireAccountToSnapshotParams(bad, nil, nil, zero, accountID, "alpaca", time.Now())
	require.Error(t, err)

	bad = baseline
	bad.Equity = "bogus"
	_, err = wireAccountToSnapshotParams(bad, nil, nil, zero, accountID, "alpaca", time.Now())
	require.Error(t, err)

	bad = baseline
	bad.BuyingPower = "bogus"
	_, err = wireAccountToSnapshotParams(bad, nil, nil, zero, accountID, "alpaca", time.Now())
	require.Error(t, err)

	bad = baseline
	bad.InitialMargin = "bogus"
	_, err = wireAccountToSnapshotParams(bad, nil, nil, zero, accountID, "alpaca", time.Now())
	require.Error(t, err)
}

func TestCorrelator_CloseIsIdempotent(t *testing.T) {
	c := newCorrelator()
	c.close()
	c.close() // must not panic on double close
	_, changed, closed := c.eventsAfter(0)
	assert.True(t, closed)
	select {
	case <-changed:
	default:
		t.Fatal("expected changed channel to already be closed")
	}
}

func TestEventReader_NextAfterClose(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	_, err = reader.Next(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestEventReader_Poll_TranslationErrorSurfaces(t *testing.T) {
	server := newFakeAlpacaServer()
	// No listings registered in the resolver: any observed order fails
	// to translate (symbol cannot be resolved), and poll must surface
	// that as an error from Next rather than silently dropping it.
	client, err := NewClient(ClientConfig{BaseURL: DefaultPaperBaseURL, Credential: StaticCredential{KeyID: "k", SecretKey: "s"}, HTTPClient: server})
	require.NoError(t, err)
	deps := testDeps(t, instrument.NewMemoryResolver())
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	broker, err := NewBroker("alpaca", client, deps, AccountConfig{AccountID: accountID})
	require.NoError(t, err)
	// Seed an order directly into the fake server (bypassing Submit,
	// which would itself fail to translate the response).
	server.orders["alpaca-1"] = wireOrder{ID: "alpaca-1", ClientOrderID: id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV").String(), Symbol: "AAPL", Qty: "1", FilledQty: "0", Type: "market", Side: "buy", TimeInForce: "day", Status: "new"}

	acc, err := broker.OpenAccount(context.Background(), accountID)
	require.NoError(t, err)
	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	_, err = reader.Next(context.Background())
	require.Error(t, err)
}

func TestEmitFillIfIncreased_MissingAvgPriceErrors(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)
	h := acc.(*accountHandle)

	o, err := order.NewOrder(order.Order{
		Request:          mustRequest(t, listing, broker.ref.AccountID),
		Status:           order.StatusFilled,
		AcceptedQuantity: quantityPtr(num.MustParseQuantity("1")),
		FilledQuantity:   num.MustParseQuantity("1"),
		BrokerOrderID:    "alpaca-1",
	})
	require.NoError(t, err)

	reader := &eventReader{account: h}
	err = reader.emitFillIfIncreased(observedOrderState{filledQty: "0"}, observedOrderState{filledQty: "1"}, o)
	require.Error(t, err)
}

func mustRequest(t *testing.T, listing instrument.Listing, accountID id.AccountID) order.Request {
	t.Helper()
	req, err := order.NewRequest(order.Proposal{
		Listing: listing, AccountID: accountID, Side: order.Buy, Type: order.Market,
		TimeInForce: order.DAY, Quantity: num.MustParseQuantity("1"),
	}, id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV"))
	require.NoError(t, err)
	return req
}

func quantityPtr(q num.Quantity) *num.Quantity { return &q }
