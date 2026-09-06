package alpaca

import (
	"context"
	"net/http"
	"testing"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountHandle_SubmitAndOpenAccount(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)

	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	req, err := order.NewRequest(order.Proposal{
		Listing:     listing,
		AccountID:   broker.ref.AccountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.DAY,
		Quantity:    num.MustParseQuantity("10"),
	}, orderID)
	require.NoError(t, err)

	o, err := acc.Submit(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, order.StatusWorking, o.Status)
	assert.Equal(t, orderID, o.Request.OrderID)

	// Resubmitting the same OrderID is idempotent: same order, no
	// duplicate created server-side.
	o2, err := acc.Submit(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, o.BrokerOrderID, o2.BrokerOrderID)
}

func TestAccountHandle_Submit_RejectsNonMarket(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	price := num.MustParsePrice("150.00")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	req, err := order.NewRequest(order.Proposal{
		Listing:     listing,
		AccountID:   broker.ref.AccountID,
		Side:        order.Buy,
		Type:        order.Limit,
		TimeInForce: order.DAY,
		Quantity:    num.MustParseQuantity("10"),
		LimitPrice:  &price,
	}, orderID)
	require.NoError(t, err)

	_, err = acc.Submit(context.Background(), req)
	require.Error(t, err)
	assert.ErrorIs(t, err, brokerpkg.ErrUnsupported)
}

func TestAccountHandle_Submit_Rejected(t *testing.T) {
	// A dedicated always-422 transport, directly exercising the
	// adapter's own translation of ErrOrderRejected into a
	// StatusRejected order.Order — real Alpaca order-level rejections
	// (insufficient buying power, invalid quantity) are not reproducible
	// through fakeAlpacaServer's own simplified submitOrder logic.
	listing := testListing(t, "AAPL")
	client, err := NewClient(ClientConfig{BaseURL: DefaultPaperBaseURL, Credential: StaticCredential{KeyID: "k", SecretKey: "s"}, HTTPClient: rejectDoer{}})
	require.NoError(t, err)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	deps := testDeps(t, testResolver(t, listing))
	b, err := NewBroker("alpaca", client, deps, AccountConfig{AccountID: accountID})
	require.NoError(t, err)
	acc, err := b.OpenAccount(context.Background(), accountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	req, err := order.NewRequest(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.DAY,
		Quantity:    num.MustParseQuantity("10"),
	}, orderID)
	require.NoError(t, err)

	o, err := acc.Submit(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, order.StatusRejected, o.Status)
	require.NotNil(t, o.Rejection)
}

func TestAccountHandle_Snapshot(t *testing.T) {
	server := newFakeAlpacaServer()
	server.positions["AAPL"] = wirePosition{Symbol: "AAPL", Side: "long", Qty: "5", AvgEntryPrice: "140.00", UnrealizedPL: "10.00"}
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	snap, err := acc.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, broker.ref.AccountID, snap.AccountID())
	require.Len(t, snap.Positions(), 1)
	assert.Equal(t, order.Long, snap.Positions()[0].Side)
	assert.True(t, snap.UnrealizedPnL().Equal(num.MustParseMoney("10.00", num.MustParseCurrency("USD"))))
}

// rejectDoer always returns a 422 response — enough for
// TestAccountHandle_Submit_Rejected's single Submit call.
type rejectDoer struct{}

func (rejectDoer) Do(req *http.Request) (*http.Response, error) {
	return jsonResponse(422, map[string]string{"message": "insufficient buying power"})
}
