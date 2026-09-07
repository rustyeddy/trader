package alpaca

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroker_Name(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer())
	assert.Equal(t, "alpaca", broker.Name())
}

func TestOrDefault(t *testing.T) {
	assert.Equal(t, "0", orDefault("", "0"))
	assert.Equal(t, "5", orDefault("5", "0"))
}

func TestWirePositionToPosition_Short(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	pos, err := wirePositionToPosition(wirePosition{Symbol: "AAPL", Side: "short", Qty: "3", AvgEntryPrice: "10.00"}, resolver, "alpaca", accountID)
	require.NoError(t, err)
	assert.Equal(t, order.Short, pos.Side)
}

// alwaysErrorDoer fails every request with a network-level error,
// exercising the "transport failure" branches distinct from a decoded
// HTTP error response.
type alwaysErrorDoer struct{}

func (alwaysErrorDoer) Do(req *http.Request) (*http.Response, error) {
	return nil, errors.New("boom")
}

func TestAccountHandle_Submit_ClosedBroker(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)
	require.NoError(t, broker.Close())

	_, err = acc.Submit(context.Background(), order.Request{})
	assert.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestAccountHandle_Submit_TransportError(t *testing.T) {
	listing := testListing(t, "AAPL")
	client, err := NewClient(ClientConfig{BaseURL: DefaultPaperBaseURL, Credential: StaticCredential{KeyID: "k", SecretKey: "s"}, HTTPClient: alwaysErrorDoer{}, MaxAttempts: 1})
	require.NoError(t, err)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	deps := testDeps(t, testResolver(t, listing))
	broker, err := NewBroker("alpaca", client, deps, AccountConfig{AccountID: accountID})
	require.NoError(t, err)
	acc, err := broker.OpenAccount(context.Background(), accountID)
	require.NoError(t, err)

	req, err := order.NewRequest(order.Proposal{
		Listing: listing, AccountID: accountID, Side: order.Buy, Type: order.Market,
		TimeInForce: order.DAY, Quantity: num.MustParseQuantity("1"),
	}, id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV"))
	require.NoError(t, err)

	_, err = acc.Submit(context.Background(), req)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrOrderRejected)
}

func TestAccountHandle_Snapshot_ClosedBroker(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)
	require.NoError(t, broker.Close())

	_, err = acc.Snapshot(context.Background())
	assert.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestAccountHandle_Snapshot_GetAccountFails(t *testing.T) {
	listing := testListing(t, "AAPL")
	client, err := NewClient(ClientConfig{BaseURL: DefaultPaperBaseURL, Credential: StaticCredential{KeyID: "k", SecretKey: "s"}, HTTPClient: alwaysErrorDoer{}, MaxAttempts: 1})
	require.NoError(t, err)
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	deps := testDeps(t, testResolver(t, listing))
	broker, err := NewBroker("alpaca", client, deps, AccountConfig{AccountID: accountID})
	require.NoError(t, err)
	acc, err := broker.OpenAccount(context.Background(), accountID)
	require.NoError(t, err)

	_, err = acc.Snapshot(context.Background())
	require.Error(t, err)
}

func TestAccountHandle_Snapshot_IncludesOpenOrders(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)

	snap, err := acc.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1)
	assert.Equal(t, orderID, snap.OpenOrders()[0].Request.OrderID)
}

func TestAccountHandle_Cancel_ClosedBroker(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)
	require.NoError(t, broker.Close())

	_, err = acc.Cancel(context.Background(), order.CancelRequest{OrderID: id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")})
	assert.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestAccountHandle_Cancel_InvalidRequest(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	_, err = acc.Cancel(context.Background(), order.CancelRequest{})
	require.Error(t, err)
}

func TestAccountHandle_Cancel_MissingEventID(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	_, err = acc.Cancel(context.Background(), order.CancelRequest{OrderID: id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")})
	require.Error(t, err)
}

func TestAccountHandle_Cancel_ContextCanceled(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = acc.Cancel(ctx, order.CancelRequest{
		OrderID:  id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestAccountHandle_Replace_ClosedBroker(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)
	require.NoError(t, broker.Close())

	newQty := num.MustParseQuantity("1")
	_, err = acc.Replace(context.Background(), order.ReplaceRequest{OrderID: id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV"), NewQuantity: &newQty})
	assert.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestAccountHandle_Replace_InvalidRequest(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	_, err = acc.Replace(context.Background(), order.ReplaceRequest{OrderID: id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")})
	require.Error(t, err)
}

func TestAccountHandle_Replace_MissingEventID(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	newQty := num.MustParseQuantity("1")
	_, err = acc.Replace(context.Background(), order.ReplaceRequest{OrderID: id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV"), NewQuantity: &newQty})
	require.Error(t, err)
}

func TestAccountHandle_Replace_ContextCanceled(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	newQty := num.MustParseQuantity("1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = acc.Replace(ctx, order.ReplaceRequest{
		OrderID: id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV"), NewQuantity: &newQty,
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestAccountHandle_Replace_DeclinedWhenAlreadyFilled(t *testing.T) {
	server := newFakeAlpacaServer()
	server.autoFill = true
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	o := submitTestOrder(t, acc, listing, broker.ref.AccountID, orderID)
	require.Equal(t, order.StatusFilled, o.Status)

	newQty := num.MustParseQuantity("1")
	result, err := acc.Replace(context.Background(), order.ReplaceRequest{
		OrderID: orderID, NewQuantity: &newQty,
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, result.Status)
	require.NotNil(t, result.Rejection)
}

func TestAccountHandle_ResolveOrderID_FallbackNotFound(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	_, err = acc.Cancel(context.Background(), order.CancelRequest{
		OrderID:  id.MustParseOrderID("ord_01BX5ZZKBKACTAV9WEVGEMMVRZ"),
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	assert.ErrorIs(t, err, brokerpkg.ErrOrderNotFound)
}

func TestFixedIntervalLimiter_Wait(t *testing.T) {
	server := newFakeAlpacaServer()
	client, err := NewClient(ClientConfig{
		BaseURL: DefaultPaperBaseURL, Credential: StaticCredential{KeyID: "k", SecretKey: "s"},
		HTTPClient: server, MinRequestInterval: time.Millisecond,
	})
	require.NoError(t, err)

	_, err = client.GetAccount(context.Background())
	require.NoError(t, err)
	_, err = client.GetAccount(context.Background())
	require.NoError(t, err)
}

func TestFixedIntervalLimiter_WaitHonorsContextCancellation(t *testing.T) {
	l := &fixedIntervalLimiter{clock: clock.Real{}, interval: time.Hour}
	require.NoError(t, l.Wait(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := l.Wait(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEventReader_EventsOnClosedBroker(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer(), testListing(t, "AAPL"))
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)
	require.NoError(t, broker.Close())

	_, err = acc.Events(context.Background(), "")
	assert.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestEmitFillIfIncreased_NoOpWhenQuantityDidNotIncrease(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)
	h := acc.(*accountHandle)

	o, err := order.NewOrder(order.Order{
		Request:          mustRequest(t, listing, broker.ref.AccountID),
		Status:           order.StatusWorking,
		AcceptedQuantity: quantityPtr(num.MustParseQuantity("1")),
	})
	require.NoError(t, err)

	err = emitFillIfIncreased(h.broker, observedOrderState{filledQty: "1"}, observedOrderState{filledQty: "1"}, o)
	require.NoError(t, err)

	// No event should have been recorded.
	events, _, _ := h.broker.corr.eventsAfter(0)
	assert.Empty(t, events)
}

func TestStatusFromWire_ReplacedIsTerminalNotPending(t *testing.T) {
	// Regression for PR #313 review: "replaced" is a terminal Alpaca
	// status distinct from "pending_replace" and must not map to
	// order.StatusPendingReplace (which would also spuriously trigger
	// wireOrderToOrder's synthesized PendingCommandID path).
	assert.Equal(t, order.StatusCanceled, statusFromWire("replaced"))
	assert.Equal(t, order.StatusPendingReplace, statusFromWire("pending_replace"))
}

func TestWireOrderToOrder_ReplacedDoesNotSynthesizePendingCommandID(t *testing.T) {
	resolver := testResolver(t, testListing(t, "AAPL"))
	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	orderID := id.MustParseOrderID("ord_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	wo := wireOrder{ID: "a1", ClientOrderID: orderID.String(), Symbol: "AAPL", Qty: "1", FilledQty: "0", Type: "market", Side: "buy", TimeInForce: "day", Status: "replaced"}
	o, err := wireOrderToOrder(wo, resolver, "alpaca", accountID, testIDs(t))
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, o.Status)
	assert.True(t, o.PendingCommandID.IsZero())
}

// TestCorrelator_AppendEventAfterCloseDoesNotPanic is a direct
// regression for PR #313 review's Copilot-identified finding: Close
// (correlator.close) closes c.changed once; a concurrent appendEvent
// that acquires c.mu afterward must observe c.closed and report
// brokerpkg.ErrClosed rather than closing the already-closed channel
// again, which would panic.
func TestCorrelator_AppendEventAfterCloseDoesNotPanic(t *testing.T) {
	c := newCorrelator()
	c.close()

	assert.NotPanics(t, func() {
		_, err := c.appendEvent(func(sequence uint64) (brokerpkg.Event, error) {
			t.Fatal("build must not be called once the correlator is closed")
			return brokerpkg.Event{}, nil
		})
		assert.ErrorIs(t, err, brokerpkg.ErrClosed)
	})
}

// TestCorrelator_CloseRacesAppendEvent exercises the actual race PR
// #313 review flagged: appendEvent and close called concurrently must
// never panic, regardless of which observes the other's state first.
// Run with -race to catch any remaining unsynchronized access.
func TestCorrelator_CloseRacesAppendEvent(t *testing.T) {
	for i := 0; i < 50; i++ {
		c := newCorrelator()
		deps := testDeps(t, testResolver(t))
		listing := testListing(t, "AAPL")
		o, err := order.NewOrder(order.Order{
			Request:          mustRequest(t, listing, id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")),
			Status:           order.StatusWorking,
			AcceptedQuantity: quantityPtr(num.MustParseQuantity("1")),
		})
		require.NoError(t, err)

		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = c.appendEvent(func(sequence uint64) (brokerpkg.Event, error) {
				return buildOrderEvent(deps, o, id.EventID{}, sequence)
			})
		}()
		assert.NotPanics(t, func() { c.close() })
		<-done
	}
}

// TestEventReader_CloseWakesBlockedNext is a direct regression for PR
// #313 review's data-race/wake-up finding: a Next call already parked
// in its blocking select must be woken by a concurrent Close, not left
// waiting out the rest of the (here, very long) poll interval.
func TestEventReader_CloseWakesBlockedNext(t *testing.T) {
	server := newFakeAlpacaServer()
	listing := testListing(t, "AAPL")
	broker := testBroker(t, server, listing)
	broker.deps.PollInterval = time.Hour // would never fire within the test
	acc, err := broker.OpenAccount(context.Background(), broker.ref.AccountID)
	require.NoError(t, err)

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := reader.Next(context.Background())
		done <- err
	}()

	// Give Next a moment to reach its blocking select before closing.
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, reader.Close())

	select {
	case err := <-done:
		assert.ErrorIs(t, err, brokerpkg.ErrClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not wake up promptly after Close")
	}
}
