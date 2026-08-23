package sim

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func testDeps() Deps {
	c := clock.NewSimulated(testStart)
	return Deps{Clock: c, IDs: id.NewGenerator(c, id.NewDeterministic(1, 2))}
}

func usd(s string) num.Money {
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
}

func mustAccountID(t *testing.T, gen *id.Generator) id.AccountID {
	t.Helper()
	aid, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	return aid
}

func mustOrderID(t *testing.T, gen *id.Generator) id.OrderID {
	t.Helper()
	oid, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	return oid
}

func mustEventID(t *testing.T, gen *id.Generator) id.EventID {
	t.Helper()
	eid, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	return eid
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

func mustRequest(t *testing.T, gen *id.Generator, accountID id.AccountID) order.Request {
	t.Helper()
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t, gen)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, gen))
	require.NoError(t, err)
	return req
}

func drainEvents(t *testing.T, reader brokerpkg.EventReader) []brokerpkg.Event {
	t.Helper()
	ctx := context.Background()
	var events []brokerpkg.Event
	for {
		e, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			return events
		}
		require.NoError(t, err)
		events = append(events, e)
	}
}

func TestNewBrokerRejectsEmptyName(t *testing.T) {
	deps := testDeps()
	_, err := NewBroker("", deps)
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewBrokerRejectsNilClock(t *testing.T) {
	deps := testDeps()
	deps.Clock = nil
	_, err := NewBroker("sim", deps)
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewBrokerRejectsNilIDs(t *testing.T) {
	deps := testDeps()
	deps.IDs = nil
	_, err := NewBroker("sim", deps)
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewBrokerRejectsZeroAccountID(t *testing.T) {
	deps := testDeps()
	_, err := NewBroker("sim", deps, AccountConfig{StartingCash: usd("10000")})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewBrokerRejectsInvalidStartingCash(t *testing.T) {
	deps := testDeps()
	_, err := NewBroker("sim", deps, AccountConfig{AccountID: mustAccountID(t, deps.IDs)})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewBrokerRejectsDuplicateAccountID(t *testing.T) {
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	_, err := NewBroker("sim", deps,
		AccountConfig{AccountID: accountID, StartingCash: usd("10000")},
		AccountConfig{AccountID: accountID, StartingCash: usd("5000")},
	)
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewBrokerWithNoAccountsIsValid(t *testing.T) {
	deps := testDeps()
	b, err := NewBroker("sim", deps)
	require.NoError(t, err)
	refs, err := b.Accounts(context.Background())
	require.NoError(t, err)
	assert.Empty(t, refs)
}

func TestBrokerNameReturnsConstructedName(t *testing.T) {
	deps := testDeps()
	b, err := NewBroker("sim-oanda", deps)
	require.NoError(t, err)
	assert.Equal(t, "sim-oanda", b.Name())
}

func TestBrokerAccountsListsConfiguredAccounts(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	a1 := mustAccountID(t, deps.IDs)
	a2 := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps,
		AccountConfig{AccountID: a1, StartingCash: usd("10000")},
		AccountConfig{AccountID: a2, StartingCash: usd("5000")},
	)
	require.NoError(t, err)

	refs, err := b.Accounts(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 2)
	ids := map[id.AccountID]bool{refs[0].AccountID: true, refs[1].AccountID: true}
	assert.True(t, ids[a1])
	assert.True(t, ids[a2])
	assert.Equal(t, "sim", refs[0].Broker)
}

func TestBrokerOpenAccountUnknownReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	b, err := NewBroker("sim", deps)
	require.NoError(t, err)

	_, err = b.OpenAccount(ctx, mustAccountID(t, deps.IDs))
	require.ErrorIs(t, err, brokerpkg.ErrAccountNotFound)
}

func TestBrokerOpenAccountReturnsDeterministicStartingSnapshot(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)

	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)
	assert.Equal(t, accountID, acc.Reference().AccountID)
	assert.Equal(t, "sim", acc.Reference().Broker)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, accountID, snap.AccountID())
	assert.Equal(t, "sim", snap.Broker())
	assert.True(t, snap.Equity().Equal(usd("10000")))
	assert.True(t, snap.BuyingPower().Equal(usd("10000")))
	assert.True(t, snap.MarginAvailable().Equal(usd("10000")))
	assert.True(t, snap.MarginUsed().Equal(usd("0")))
	assert.True(t, snap.RealizedPnL().Equal(usd("0")))
	assert.True(t, snap.UnrealizedPnL().Equal(usd("0")))
	assert.Equal(t, testStart, snap.AsOf())
	assert.Empty(t, snap.Positions())
	assert.Empty(t, snap.OpenOrders())
}

func TestTwoBrokersFromSameConfigProduceIdenticalStartingSnapshots(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t, id.NewGenerator(clock.NewSimulated(testStart), id.NewDeterministic(1, 2)))

	newSnap := func() account.Snapshot {
		deps := testDeps()
		b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
		require.NoError(t, err)
		acc, err := b.OpenAccount(ctx, accountID)
		require.NoError(t, err)
		snap, err := acc.Snapshot(ctx)
		require.NoError(t, err)
		return snap
	}

	s1 := newSnap()
	s2 := newSnap()
	assert.Equal(t, s1.AccountID(), s2.AccountID())
	assert.Equal(t, s1.AsOf(), s2.AsOf())
	assert.True(t, s1.Equity().Equal(s2.Equity()))
	assert.True(t, s1.CashBalances()[0].Equal(s2.CashBalances()[0]))
}

func TestMultipleAccountsAreIsolated(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	a1 := mustAccountID(t, deps.IDs)
	a2 := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps,
		AccountConfig{AccountID: a1, StartingCash: usd("10000")},
		AccountConfig{AccountID: a2, StartingCash: usd("5000")},
	)
	require.NoError(t, err)

	acc1, err := b.OpenAccount(ctx, a1)
	require.NoError(t, err)
	acc2, err := b.OpenAccount(ctx, a2)
	require.NoError(t, err)

	req := mustRequest(t, deps.IDs, a1)
	_, err = acc1.Submit(ctx, req)
	require.NoError(t, err)

	snap1, err := acc1.Snapshot(ctx)
	require.NoError(t, err)
	snap2, err := acc2.Snapshot(ctx)
	require.NoError(t, err)

	assert.Len(t, snap1.OpenOrders(), 1)
	assert.Empty(t, snap2.OpenOrders(), "submitting against account 1 must not leak into account 2's snapshot")
	assert.True(t, snap2.Equity().Equal(usd("5000")), "account 2's balance must be unaffected by account 1's activity")
}

func TestBrokerCloseRejectsFurtherUse(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	require.NoError(t, b.Close())
	require.NoError(t, b.Close()) // idempotent

	_, err = b.Accounts(ctx)
	require.ErrorIs(t, err, brokerpkg.ErrClosed)

	_, err = b.OpenAccount(ctx, accountID)
	require.ErrorIs(t, err, brokerpkg.ErrClosed)

	_, err = acc.Snapshot(ctx)
	require.ErrorIs(t, err, brokerpkg.ErrClosed)

	_, err = acc.Submit(ctx, mustRequest(t, deps.IDs, accountID))
	require.ErrorIs(t, err, brokerpkg.ErrClosed)

	_, err = acc.Cancel(ctx, order.CancelRequest{OrderID: mustOrderID(t, deps.IDs), Metadata: id.Metadata{EventID: mustEventID(t, deps.IDs)}})
	require.ErrorIs(t, err, brokerpkg.ErrClosed)

	qty := num.MustParseQuantity("1")
	_, err = acc.Replace(ctx, order.ReplaceRequest{OrderID: mustOrderID(t, deps.IDs), NewQuantity: &qty, Metadata: id.Metadata{EventID: mustEventID(t, deps.IDs)}})
	require.ErrorIs(t, err, brokerpkg.ErrClosed)

	_, err = acc.Events(ctx, "")
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestAccountSubmitAcceptsOrderIntoWorkingWithoutFill(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, deps.IDs, accountID)
	o, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, order.StatusWorking, o.Status)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(req.Quantity))
	assert.Zero(t, o.FilledQuantity.Cmp(num.MustParseQuantity("0")), "no fill matching happens in this package yet")
	assert.Equal(t, "sim-"+req.OrderID.String(), o.BrokerOrderID)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1)
	assert.Equal(t, req.OrderID, snap.OpenOrders()[0].Request.OrderID)
	assert.True(t, snap.Equity().Equal(usd("10000")), "no cash effect until a real fill exists")
}

func TestAccountSubmitIsIdempotentOnOrderID(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, deps.IDs, accountID)
	first, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	second, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, first.Request.OrderID, second.Request.OrderID)
	assert.Equal(t, first.BrokerOrderID, second.BrokerOrderID)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	events := drainEvents(t, reader)
	require.Len(t, events, 1, "a resubmission of the same OrderID must not emit a second event")
}

func TestAccountSubmitRejectsInvalidRequest(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	_, err = acc.Submit(ctx, order.Request{}) // zero value fails order.NewRequest's validation chain
	require.Error(t, err)
}

func TestAccountSubmitReturnedOrderDoesNotAliasStoredState(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, deps.IDs, accountID)
	o, err := acc.Submit(ctx, req)
	require.NoError(t, err)

	mutated := num.MustParseQuantity("999999")
	*o.AcceptedQuantity = mutated

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1)
	require.NotNil(t, snap.OpenOrders()[0].AcceptedQuantity)
	assert.True(t, snap.OpenOrders()[0].AcceptedQuantity.Equal(req.Quantity),
		"mutating the caller's returned Order must not affect the account's internally stored state")
}

func TestAccountSubmitLimitOrderAcceptsRequestedPrice(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	limitPrice := num.MustParsePrice("1.09000")
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Limit,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  &limitPrice,
		Metadata:    id.Metadata{EventID: mustEventID(t, deps.IDs)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, deps.IDs))
	require.NoError(t, err)

	o, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, o.AcceptedLimitPrice)
	assert.True(t, o.AcceptedLimitPrice.Equal(limitPrice))

	// Mutating the returned Order's accepted price must not affect the
	// account's internally stored copy — the same aliasing guarantee
	// TestAccountSubmitReturnedOrderDoesNotAliasStoredState covers for
	// AcceptedQuantity.
	mutated := num.MustParsePrice("9.99999")
	*o.AcceptedLimitPrice = mutated

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1)
	require.NotNil(t, snap.OpenOrders()[0].AcceptedLimitPrice)
	assert.True(t, snap.OpenOrders()[0].AcceptedLimitPrice.Equal(limitPrice))
}

func TestAccountSubmitStopOrderAcceptsRequestedPrice(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	stopPrice := num.MustParsePrice("1.08000")
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Sell,
		Type:        order.Stop,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		StopPrice:   &stopPrice,
		Metadata:    id.Metadata{EventID: mustEventID(t, deps.IDs)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, deps.IDs))
	require.NoError(t, err)

	o, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, o.AcceptedStopPrice)
	assert.True(t, o.AcceptedStopPrice.Equal(stopPrice))
	assert.Nil(t, o.AcceptedLimitPrice)
}

func TestAccountCancelReturnsUnsupported(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	cancelReq, err := order.NewCancelRequest(order.CancelRequest{OrderID: req.OrderID, Metadata: id.Metadata{EventID: mustEventID(t, deps.IDs)}})
	require.NoError(t, err)
	_, err = acc.Cancel(ctx, cancelReq)
	require.ErrorIs(t, err, brokerpkg.ErrUnsupported)
}

func TestAccountReplaceReturnsUnsupported(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	qty := num.MustParseQuantity("500")
	replaceReq, err := order.NewReplaceRequest(order.ReplaceRequest{OrderID: req.OrderID, NewQuantity: &qty, Metadata: id.Metadata{EventID: mustEventID(t, deps.IDs)}})
	require.NoError(t, err)
	_, err = acc.Replace(ctx, replaceReq)
	require.ErrorIs(t, err, brokerpkg.ErrUnsupported)
}

func TestAccountEventsDeterministicOrderAndReplay(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req1 := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req1)
	require.NoError(t, err)
	req2 := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req2)
	require.NoError(t, err)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	events := drainEvents(t, reader)
	require.Len(t, events, 2)
	assert.Equal(t, uint64(1), events[0].Sequence)
	assert.Equal(t, uint64(2), events[1].Sequence)
	assert.Equal(t, req1.OrderID, events[0].Order.Request.OrderID)
	assert.Equal(t, req2.OrderID, events[1].Order.Request.OrderID)

	reader2, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader2.Close() }()
	replay := drainEvents(t, reader2)
	assert.Equal(t, events, replay)
}

func TestAccountEventsResumeFromCursorSkipsDelivered(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req1 := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req1)
	require.NoError(t, err)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	first := drainEvents(t, reader)
	require.Len(t, first, 1)
	_ = reader.Close()

	req2 := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req2)
	require.NoError(t, err)

	cursor := encodeCursor(first[len(first)-1].Sequence)
	resumed, err := acc.Events(ctx, cursor)
	require.NoError(t, err)
	defer func() { _ = resumed.Close() }()
	second := drainEvents(t, resumed)
	require.Len(t, second, 1)
	assert.Equal(t, uint64(2), second[0].Sequence)
}

func TestAccountEventsReaderCloseThenNextReturnsClosed(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.NoError(t, reader.Close())

	_, err = reader.Next(ctx)
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestAccountEventsNextRespectsCanceledContext(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = reader.Next(canceledCtx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAccountEventsNextRespectsContextCanceledWhileBlockedOnMutex(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	r := reader.(*eventReader)

	cancelCtx, cancel := context.WithCancel(ctx)

	release := make(chan struct{})
	r.mu.Lock()
	go func() {
		<-release
		r.mu.Unlock()
	}()

	done := make(chan struct{})
	var nextErr error
	go func() {
		_, nextErr = r.Next(cancelCtx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	close(release)

	<-done
	require.ErrorIs(t, nextErr, context.Canceled)
}

func TestCloneOrderClonesEveryPointerAndSliceField(t *testing.T) {
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	listing := mustEurUsdListing(t)

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
		Metadata:    id.Metadata{EventID: mustEventID(t, deps.IDs)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, deps.IDs))
	require.NoError(t, err)

	accepted := num.MustParseQuantity("1000")
	acceptedLimit := num.MustParsePrice("1.09000")
	acceptedStop := num.MustParsePrice("1.08000")
	avgFill := num.MustParsePrice("1.09000")
	fillID, err := id.GenerateFillID(deps.IDs)
	require.NoError(t, err)

	original, err := order.NewOrder(order.Order{
		Request:              req,
		BrokerOrderID:        "sim-test",
		AcceptedQuantity:     &accepted,
		AcceptedLimitPrice:   &acceptedLimit,
		AcceptedStopPrice:    &acceptedStop,
		AvgFillPrice:         &avgFill,
		Status:               order.StatusPartiallyFilled,
		FilledQuantity:       num.MustParseQuantity("100"),
		AppliedFillIDs:       []id.FillID{fillID},
		AppliedBrokerFillIDs: []string{"broker-fill-1"},
	})
	require.NoError(t, err)

	clone := cloneOrder(original)
	assert.Equal(t, original, clone)

	// Mutating every pointer/slice field on the clone must not affect
	// the original — proving independent allocation, not merely equal
	// values.
	*clone.Request.LimitPrice = num.MustParsePrice("0.00001")
	*clone.Request.StopPrice = num.MustParsePrice("0.00001")
	*clone.AcceptedQuantity = num.MustParseQuantity("1")
	*clone.AcceptedLimitPrice = num.MustParsePrice("0.00001")
	*clone.AcceptedStopPrice = num.MustParsePrice("0.00001")
	*clone.AvgFillPrice = num.MustParsePrice("0.00001")
	clone.AppliedFillIDs[0] = id.FillID{}
	clone.AppliedBrokerFillIDs[0] = "mutated"

	assert.True(t, original.Request.LimitPrice.Equal(limitPrice))
	assert.True(t, original.Request.StopPrice.Equal(stopPrice))
	assert.True(t, original.AcceptedQuantity.Equal(accepted))
	assert.True(t, original.AcceptedLimitPrice.Equal(acceptedLimit))
	assert.True(t, original.AcceptedStopPrice.Equal(acceptedStop))
	assert.True(t, original.AvgFillPrice.Equal(avgFill))
	assert.Equal(t, fillID, original.AppliedFillIDs[0])
	assert.Equal(t, "broker-fill-1", original.AppliedBrokerFillIDs[0])
}

func TestCloneOrderClonesRejection(t *testing.T) {
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	req := mustRequest(t, deps.IDs, accountID)

	original, err := order.NewOrder(order.Order{
		Request: req,
		Status:  order.StatusRejected,
		Rejection: &order.Rejection{
			Reason:     order.ReasonInsufficientMargin,
			Detail:     "not enough margin",
			BrokerCode: "E-102",
		},
	})
	require.NoError(t, err)

	clone := cloneOrder(original)
	require.NotNil(t, clone.Rejection)
	assert.Equal(t, *original.Rejection, *clone.Rejection)

	clone.Rejection.Detail = "mutated"
	assert.Equal(t, "not enough margin", original.Rejection.Detail)
}

func TestDecodeCursorMalformedStringDefaultsToZero(t *testing.T) {
	assert.Equal(t, uint64(0), decodeCursor(brokerpkg.EventCursor("not-a-number")))
	assert.Equal(t, uint64(0), decodeCursor(brokerpkg.EventCursor("")))
	assert.Equal(t, uint64(3), decodeCursor(encodeCursor(3)))
}
