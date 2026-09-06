package alpaca

import (
	"context"
	"io"
	"testing"
	"time"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventReader_ObservesSubmitEventImmediately(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	o := submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)
	require.Equal(t, order.StatusWorking, o.Status)

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	ev, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, brokerpkg.EventKindOrder, ev.Kind)
	require.NotNil(t, ev.Order)
	assert.Equal(t, orderID, ev.Order.Request.OrderID)
	assert.Equal(t, uint64(1), ev.Sequence)
}

func TestEventReader_PollDetectsFillTransition(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	// Fast polling for the test.
	broker.deps.PollInterval = time.Millisecond
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	o := submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)
	require.Equal(t, order.StatusWorking, o.Status)

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	// First event: the synchronous order-accepted event from Submit.
	ev1, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, brokerpkg.EventKindOrder, ev1.Kind)

	// Simulate Alpaca settling the fill asynchronously.
	server.settleFill(o.BrokerOrderID, "10", "150.25")

	// Next Next() call should poll, discover the transition, and
	// deliver an order-status event followed by a synthesized fill.
	ev2, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, brokerpkg.EventKindOrder, ev2.Kind)
	assert.Equal(t, order.StatusFilled, ev2.Order.Status)

	ev3, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, brokerpkg.EventKindFill, ev3.Kind)
	require.NotNil(t, ev3.Fill)
	assert.True(t, ev3.Fill.Quantity.Equal(num.MustParseQuantity("10")))
	assert.True(t, ev3.Fill.Price.Equal(num.MustParsePrice("150.25")))

	assert.Greater(t, ev3.Sequence, ev2.Sequence)
	assert.Greater(t, ev2.Sequence, ev1.Sequence)
}

func TestEventReader_NextHonorsContextCancellation(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	broker.deps.PollInterval = time.Hour // never fires within the test
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reader.Next(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEventReader_ReturnsEOFAfterBrokerClose(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	require.NoError(t, broker.Close())

	_, err = reader.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)
}

func TestEventReader_CursorResumesAfterLastDelivered(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	ev, err := reader.Next(context.Background())
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	cursor := encodeCursor(ev.Sequence)
	reader2, err := acc.Events(context.Background(), cursor)
	require.NoError(t, err)
	defer func() { _ = reader2.Close() }()

	require.NoError(t, broker.Close())
	_, err = reader2.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)
}

func TestEventReader_PollDetectsCancelSettlement(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	broker.deps.PollInterval = time.Millisecond
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	o := submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	ev1, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, brokerpkg.EventKindOrder, ev1.Kind)

	cancelResult, err := acc.Cancel(context.Background(), order.CancelRequest{
		OrderID:  orderID,
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	require.NoError(t, err)
	assert.Equal(t, order.StatusPendingCancel, cancelResult.Status)

	// Cancel's own synchronous event.
	ev2, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, order.StatusPendingCancel, ev2.Order.Status)

	// Simulate Alpaca finishing the asynchronous cancel.
	server.settleCancel(o.BrokerOrderID)

	ev3, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, ev3.Order.Status)
}
