package alpaca

import (
	"context"
	"testing"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func submitTestOrder(t *testing.T, acc brokerpkg.Account, listing instrument.Listing, accountID id.AccountID, orderID id.OrderID) order.Order {
	t.Helper()
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
	return o
}

func TestAccountHandle_Cancel_Success(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	o := submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)
	require.Equal(t, order.StatusWorking, o.Status)

	result, err := acc.Cancel(context.Background(), order.CancelRequest{
		OrderID:  orderID,
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	require.NoError(t, err)
	assert.Equal(t, order.StatusPendingCancel, result.Status)
	assert.Nil(t, result.Rejection)
}

func TestAccountHandle_Cancel_NotFound(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	_, err = acc.Cancel(context.Background(), order.CancelRequest{
		OrderID:  id.MustParseOrderID("ord_01BX5ZZKBKACTAV9WEVGEMMVRZ"),
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	assert.ErrorIs(t, err, brokerpkg.ErrOrderNotFound)
}

func TestAccountHandle_Cancel_DeclinedWhenAlreadyFilled(t *testing.T) {
	server := newFakeAlpacaServer()
	server.autoFill = true
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	o := submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)
	require.Equal(t, order.StatusFilled, o.Status)

	result, err := acc.Cancel(context.Background(), order.CancelRequest{
		OrderID:  orderID,
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, result.Status)
	require.NotNil(t, result.Rejection)
}

func TestAccountHandle_Replace_Success(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)

	newQty := num.MustParseQuantity("5")
	result, err := acc.Replace(context.Background(), order.ReplaceRequest{
		OrderID:     orderID,
		NewQuantity: &newQty,
		Metadata:    id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	require.NoError(t, err)
	assert.Equal(t, order.StatusPendingReplace, result.Status)
	assert.Nil(t, result.Rejection)
}

func TestAccountHandle_Replace_NotFound(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	newQty := num.MustParseQuantity("5")
	_, err = acc.Replace(context.Background(), order.ReplaceRequest{
		OrderID:     id.MustParseOrderID("ord_01BX5ZZKBKACTAV9WEVGEMMVRZ"),
		NewQuantity: &newQty,
		Metadata:    id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	assert.ErrorIs(t, err, brokerpkg.ErrOrderNotFound)
}

func TestAccountHandle_CancelReplace_ResolveByClientOrderIDFallback(t *testing.T) {
	// Simulates a fresh accountHandle (e.g. after a process restart)
	// with no local brokerOrderIDs entry — Cancel/Replace must still
	// resolve via GetOrderByClientOrderID.
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)

	// A second handle on the same broker (correlator state is shared —
	// simulate the "no local memory" case by clearing it directly).
	broker.corr.brokerOrderIDs = map[id.OrderID]string{}

	result, err := acc.Cancel(context.Background(), order.CancelRequest{
		OrderID:  orderID,
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	require.NoError(t, err)
	assert.Equal(t, order.StatusPendingCancel, result.Status)
}
