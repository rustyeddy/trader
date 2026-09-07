package eq09

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// This file unit-tests helpers_test.go's pure logic against fakes,
// with no network access — real coverage that runs on every
// `go test ./examples/eq09/...`, unlike smoke_test.go's own
// "alpacasmoke"-gated live path.

func testSPYListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"), num.MustParseQuantity("1"), num.MustParseRate("1"), num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst, Provider: "alpaca", Symbol: "SPY", Spec: spec, Tradable: true,
	})
	require.NoError(t, err)
	return listing
}

func testAccountID(t *testing.T) id.AccountID {
	t.Helper()
	gen := id.NewGenerator(clock.Real{}, id.Random{})
	accID, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	return accID
}

func TestIsFlatSPY_EmptyPositions(t *testing.T) {
	listing := testSPYListing(t)
	accID := testAccountID(t)
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID: accID, Broker: "alpaca", Currency: num.MustParseCurrency("USD"), AsOf: time.Now(),
		Equity: num.MustParseMoney("0", num.MustParseCurrency("USD")), BuyingPower: num.MustParseMoney("0", num.MustParseCurrency("USD")),
		MarginUsed: num.MustParseMoney("0", num.MustParseCurrency("USD")), MarginAvailable: num.MustParseMoney("0", num.MustParseCurrency("USD")),
		RealizedPnL: num.MustParseMoney("0", num.MustParseCurrency("USD")), UnrealizedPnL: num.MustParseMoney("0", num.MustParseCurrency("USD")),
		Fees: num.MustParseMoney("0", num.MustParseCurrency("USD")), Financing: num.MustParseMoney("0", num.MustParseCurrency("USD")),
	})
	require.NoError(t, err)
	assert.True(t, isFlatSPY(snap, listing))
	assertFlatSPYPosition(t, snap, listing)
}

func TestIsFlatSPY_OpenPosition(t *testing.T) {
	listing := testSPYListing(t)
	accID := testAccountID(t)
	avgPrice := num.MustParsePrice("450.00")
	pos, err := order.NewPosition(order.Position{
		AccountID: accID, Listing: listing, Side: order.Long, Quantity: num.MustParseQuantity("1"), AvgPrice: &avgPrice,
	})
	require.NoError(t, err)
	zero := num.MustParseMoney("0", num.MustParseCurrency("USD"))
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID: accID, Broker: "alpaca", Currency: num.MustParseCurrency("USD"), AsOf: time.Now(),
		Equity: zero, BuyingPower: zero, MarginUsed: zero, MarginAvailable: zero,
		RealizedPnL: zero, UnrealizedPnL: zero, Fees: zero, Financing: zero,
		Positions: []order.Position{pos},
	})
	require.NoError(t, err)
	assert.False(t, isFlatSPY(snap, listing))
}

// fakeEventReader is a minimal, in-memory broker.EventReader for
// testing waitForFill without any network or real broker.
type fakeEventReader struct {
	events []brokerpkg.Event
	idx    int
}

func (f *fakeEventReader) Next(ctx context.Context) (brokerpkg.Event, error) {
	if err := ctx.Err(); err != nil {
		return brokerpkg.Event{}, err
	}
	if f.idx >= len(f.events) {
		<-ctx.Done()
		return brokerpkg.Event{}, ctx.Err()
	}
	ev := f.events[f.idx]
	f.idx++
	return ev, nil
}

func (f *fakeEventReader) Close() error { return nil }

var _ brokerpkg.EventReader = (*fakeEventReader)(nil)

func testOrderEvent(t *testing.T, orderID id.OrderID, status order.Status) brokerpkg.Event {
	t.Helper()
	listing := testSPYListing(t)
	accID := testAccountID(t)
	req, err := order.NewRequest(order.Proposal{
		Listing: listing, AccountID: accID, Side: order.Buy, Type: order.Market,
		TimeInForce: order.DAY, Quantity: num.MustParseQuantity("1"),
	}, orderID)
	require.NoError(t, err)
	qty := num.MustParseQuantity("1")
	o := order.Order{Request: req, Status: status}
	if status.Terminal() || status == order.StatusWorking {
		o.AcceptedQuantity = &qty
	}
	built, err := order.NewOrder(o)
	require.NoError(t, err)

	eventID, err := id.GenerateEventID(id.NewGenerator(clock.Real{}, id.Random{}))
	require.NoError(t, err)
	ev, err := brokerpkg.NewEvent(brokerpkg.Event{
		Metadata:   id.Metadata{EventID: eventID, Timestamp: time.Now()},
		ObservedAt: time.Now(),
		Sequence:   1,
		Kind:       brokerpkg.EventKindOrder,
		Order:      &built,
	})
	require.NoError(t, err)
	return ev
}

func testFillEvent(t *testing.T, orderID id.OrderID) brokerpkg.Event {
	t.Helper()
	listing := testSPYListing(t)
	accID := testAccountID(t)
	gen := id.NewGenerator(clock.Real{}, id.Random{})
	fillID, err := id.GenerateFillID(gen)
	require.NoError(t, err)
	fill, err := order.NewFill(order.Fill{
		FillID: fillID, OrderID: orderID, AccountID: accID, Listing: listing,
		Side: order.Buy, Price: num.MustParsePrice("450.00"), Quantity: num.MustParseQuantity("1"),
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	ev, err := brokerpkg.NewEvent(brokerpkg.Event{
		Metadata:   id.Metadata{EventID: eventID, Timestamp: time.Now()},
		ObservedAt: time.Now(),
		Sequence:   1,
		Kind:       brokerpkg.EventKindFill,
		Fill:       &fill,
	})
	require.NoError(t, err)
	return ev
}

func TestWaitForFill_ReturnsTrueOnFilledWithFillEvent(t *testing.T) {
	realOrderID := mustOrderID(t)
	reader := &fakeEventReader{events: []brokerpkg.Event{
		testOrderEvent(t, realOrderID, order.StatusWorking),
		testFillEvent(t, realOrderID),
		testOrderEvent(t, realOrderID, order.StatusFilled),
	}}
	got := waitForFill(t, context.Background(), reader, realOrderID, time.Second)
	assert.True(t, got)
}

// TestWaitForFill_FalseWhenStatusFilledWithoutFillEvent locks in PR
// #314 review's requirement: Order.Status alone is not proof of a
// fill. An order reporting StatusFilled with no corresponding
// EventKindFill ever observed must not be treated as a verified fill.
func TestWaitForFill_FalseWhenStatusFilledWithoutFillEvent(t *testing.T) {
	realOrderID := mustOrderID(t)
	reader := &fakeEventReader{events: []brokerpkg.Event{
		testOrderEvent(t, realOrderID, order.StatusWorking),
		testOrderEvent(t, realOrderID, order.StatusFilled),
	}}
	got := waitForFill(t, context.Background(), reader, realOrderID, 20*time.Millisecond)
	assert.False(t, got)
}

func TestWaitForFill_ReturnsFalseOnCanceled(t *testing.T) {
	realOrderID := mustOrderID(t)
	reader := &fakeEventReader{events: []brokerpkg.Event{
		testOrderEvent(t, realOrderID, order.StatusWorking),
		testOrderEvent(t, realOrderID, order.StatusCanceled),
	}}
	got := waitForFill(t, context.Background(), reader, realOrderID, time.Second)
	assert.False(t, got)
}

func TestWaitForFill_IgnoresOtherOrders(t *testing.T) {
	realOrderID := mustOrderID(t)
	otherOrderID := mustOrderID(t)
	reader := &fakeEventReader{events: []brokerpkg.Event{
		testOrderEvent(t, otherOrderID, order.StatusFilled),
		testFillEvent(t, otherOrderID),
		testOrderEvent(t, realOrderID, order.StatusFilled),
		testFillEvent(t, realOrderID),
	}}
	got := waitForFill(t, context.Background(), reader, realOrderID, time.Second)
	assert.True(t, got)
}

func TestWaitForFill_TimesOut(t *testing.T) {
	realOrderID := mustOrderID(t)
	reader := &fakeEventReader{events: []brokerpkg.Event{
		testOrderEvent(t, realOrderID, order.StatusWorking),
	}}
	got := waitForFill(t, context.Background(), reader, realOrderID, 20*time.Millisecond)
	assert.False(t, got)
}

func mustOrderID(t *testing.T) id.OrderID {
	t.Helper()
	gen := id.NewGenerator(clock.Real{}, id.Random{})
	orderID, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	return orderID
}

func TestMustFreshEventID(t *testing.T) {
	gen := id.NewGenerator(clock.Real{}, id.Random{})
	got := mustFreshEventID(t, gen)
	assert.False(t, got.IsZero())
}

// fakeAccount is a minimal broker.Account for testing
// cancelAndAwaitTerminal without any network or real broker; only
// Cancel is ever called by that function, so every other method
// panics if reached, catching an accidental dependency on it.
type fakeAccount struct {
	cancelResult order.CancelResult
	cancelErr    error
}

func (f *fakeAccount) Reference() account.Reference { panic("not used by cancelAndAwaitTerminal") }
func (f *fakeAccount) Snapshot(context.Context) (account.Snapshot, error) {
	panic("not used by cancelAndAwaitTerminal")
}
func (f *fakeAccount) Submit(context.Context, order.Request) (order.Order, error) {
	panic("not used by cancelAndAwaitTerminal")
}
func (f *fakeAccount) Cancel(context.Context, order.CancelRequest) (order.CancelResult, error) {
	return f.cancelResult, f.cancelErr
}
func (f *fakeAccount) Replace(context.Context, order.ReplaceRequest) (order.ReplaceResult, error) {
	panic("not used by cancelAndAwaitTerminal")
}
func (f *fakeAccount) Events(context.Context, brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	panic("not used by cancelAndAwaitTerminal")
}

var _ brokerpkg.Account = (*fakeAccount)(nil)

func TestCancelAndAwaitTerminal_ReturnsCanceled(t *testing.T) {
	orderID := mustOrderID(t)
	acc := &fakeAccount{cancelResult: order.CancelResult{OrderID: orderID, Status: order.StatusPendingCancel}}
	reader := &fakeEventReader{events: []brokerpkg.Event{
		testOrderEvent(t, orderID, order.StatusWorking),
		testOrderEvent(t, orderID, order.StatusCanceled),
	}}
	got := cancelAndAwaitTerminal(t, context.Background(), acc, reader, testGenerator(), orderID, time.Second)
	assert.Equal(t, order.StatusCanceled, got)
}

// TestCancelAndAwaitTerminal_DetectsRaceFill is the exact scenario PR
// #314 review flagged: a cancel request races with a real fill.
// cancelAndAwaitTerminal must report the order's true final status
// (StatusFilled), never the synchronous CancelResult's own
// StatusPendingCancel, so a caller can correctly decide to flatten the
// resulting position instead of assuming the cancel succeeded.
func TestCancelAndAwaitTerminal_DetectsRaceFill(t *testing.T) {
	orderID := mustOrderID(t)
	acc := &fakeAccount{cancelResult: order.CancelResult{OrderID: orderID, Status: order.StatusPendingCancel}}
	reader := &fakeEventReader{events: []brokerpkg.Event{
		testOrderEvent(t, orderID, order.StatusFilled),
	}}
	got := cancelAndAwaitTerminal(t, context.Background(), acc, reader, testGenerator(), orderID, time.Second)
	assert.Equal(t, order.StatusFilled, got)
}

func testGenerator() *id.Generator {
	return id.NewGenerator(clock.Real{}, id.Random{})
}
