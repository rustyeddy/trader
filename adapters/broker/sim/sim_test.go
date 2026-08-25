package sim

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
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

// fixedPriceSource is a deterministic FillPriceSource keyed by listing
// symbol, ignoring side: tests that need bid/ask asymmetry construct
// their own FillPriceSource instead.
type fixedPriceSource map[string]num.Price

func (f fixedPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := f[listing.Symbol()]
	if !ok {
		return num.Price{}, fmt.Errorf("fixedPriceSource: no price for %s", listing.Symbol())
	}
	return p, nil
}

func testDeps() Deps {
	c := clock.NewSimulated(testStart)
	return Deps{
		Clock: c,
		IDs:   id.NewGenerator(c, id.NewDeterministic(1, 2)),
		Prices: fixedPriceSource{
			"EUR_USD": num.MustParsePrice("1.10000"),
			"GBP_USD": num.MustParsePrice("1.25000"),
		},
	}
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

func mustGbpUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
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
		Symbol:     "GBP_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// mustEurGbpListing settles in GBP, deliberately not USD — used to
// exercise ErrUnsupportedSettlementCurrency against the USD test
// accounts every other fixture listing uses.
func mustEurGbpListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("GBP"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("GBP"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "EUR_GBP",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// mustRequest builds a Limit order request: this package's generic
// lifecycle/idempotency/event-ordering tests use it because a Limit
// order stays StatusWorking with no fill matching (issue #150, M3-07,
// not this package's scope yet), keeping those tests' behavior
// unaffected by market-order fill semantics (issue #149). Tests that
// specifically exercise market-order fills use mustMarketRequest.
func mustRequest(t *testing.T, gen *id.Generator, accountID id.AccountID) order.Request {
	t.Helper()
	limitPrice := num.MustParsePrice("1.10000")
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Limit,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  &limitPrice,
		Metadata:    id.Metadata{EventID: mustEventID(t, gen)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, gen))
	require.NoError(t, err)
	return req
}

// mustMarketRequest builds a Market order request for side against
// accountID on the standard EUR/USD test listing.
func mustMarketRequest(t *testing.T, gen *id.Generator, accountID id.AccountID, side order.Side, quantity string) order.Request {
	t.Helper()
	return mustMarketRequestFor(t, gen, accountID, mustEurUsdListing(t), side, quantity)
}

func mustMarketRequestFor(t *testing.T, gen *id.Generator, accountID id.AccountID, listing instrument.Listing, side order.Side, quantity string) order.Request {
	t.Helper()
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        side,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity(quantity),
		Metadata:    id.Metadata{EventID: mustEventID(t, gen)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, gen))
	require.NoError(t, err)
	return req
}

func mustLimitRequest(t *testing.T, gen *id.Generator, accountID id.AccountID, side order.Side, quantity, limitPrice string) order.Request {
	t.Helper()
	return mustLimitRequestFor(t, gen, accountID, mustEurUsdListing(t), side, quantity, limitPrice)
}

func mustLimitRequestFor(t *testing.T, gen *id.Generator, accountID id.AccountID, listing instrument.Listing, side order.Side, quantity, limitPrice string) order.Request {
	t.Helper()
	price := num.MustParsePrice(limitPrice)
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        side,
		Type:        order.Limit,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity(quantity),
		LimitPrice:  &price,
		Metadata:    id.Metadata{EventID: mustEventID(t, gen)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, gen))
	require.NoError(t, err)
	return req
}

func mustStopRequest(t *testing.T, gen *id.Generator, accountID id.AccountID, side order.Side, quantity, stopPrice string) order.Request {
	t.Helper()
	return mustStopRequestFor(t, gen, accountID, mustEurUsdListing(t), side, quantity, stopPrice)
}

func mustStopRequestFor(t *testing.T, gen *id.Generator, accountID id.AccountID, listing instrument.Listing, side order.Side, quantity, stopPrice string) order.Request {
	t.Helper()
	price := num.MustParsePrice(stopPrice)
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        side,
		Type:        order.Stop,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity(quantity),
		StopPrice:   &price,
		Metadata:    id.Metadata{EventID: mustEventID(t, gen)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, gen))
	require.NoError(t, err)
	return req
}

func mustObservation(t *testing.T, listing instrument.Listing, openS, highS, lowS, closeS string, at time.Time) Observation {
	t.Helper()
	obs := Observation{
		Listing: listing,
		Open:    num.MustParsePrice(openS),
		High:    num.MustParsePrice(highS),
		Low:     num.MustParsePrice(lowS),
		Close:   num.MustParsePrice(closeS),
		Time:    at,
	}
	require.NoError(t, obs.validate())
	return obs
}

// drainEvents reads exactly n events from reader. Next blocks rather
// than returning io.EOF merely because it has caught up (ADR-024), so
// callers must already know how many events to expect — there is no
// "read whatever is currently available" call that terminates on its
// own short of the account's owning Broker being closed.
func drainEvents(t *testing.T, reader brokerpkg.EventReader, n int) []brokerpkg.Event {
	t.Helper()
	ctx := context.Background()
	events := make([]brokerpkg.Event, 0, n)
	for range n {
		e, err := reader.Next(ctx)
		require.NoError(t, err)
		events = append(events, e)
	}
	return events
}

// assertNoMoreEventsSoon confirms reader does not deliver another event
// within a short window, without asserting exactly what would happen
// if it waited forever (which would only be resolved by a later Submit
// or the owning Broker closing).
func assertNoMoreEventsSoon(t *testing.T, reader brokerpkg.EventReader) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := reader.Next(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
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

func TestNewBrokerRejectsNilPrices(t *testing.T) {
	deps := testDeps()
	deps.Prices = nil
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

// TestAccountSubmitAcceptsOrderIntoWorkingWithoutFill covers a Limit
// order specifically: limit/stop trigger semantics are issue #150's
// scope (M3-07), not this package's yet, so a Limit order always stays
// StatusWorking with no fill matching, unlike a Market order (issue
// #149, M3-06 — see TestAccountSubmitMarketBuyFillsImmediatelyAndOpensLongPosition).
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
	assert.Zero(t, o.FilledQuantity.Cmp(num.MustParseQuantity("0")), "limit orders do not fill in this package yet (issue #150)")
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
	events := drainEvents(t, reader, 1)
	require.Len(t, events, 1)
	assertNoMoreEventsSoon(t, reader) // a resubmission of the same OrderID must not emit a second event
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

// TestAccountSubmitMarketBuyFillsImmediatelyAndOpensLongPosition covers
// #149's core vertical slice: submit -> acknowledgement -> order/fill
// events -> updated snapshot, for a market buy against a flat account.
func TestAccountSubmitMarketBuyFillsImmediatelyAndOpensLongPosition(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	o, err := acc.Submit(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, order.StatusFilled, o.Status)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.FilledQuantity.Equal(*o.AcceptedQuantity))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.OpenOrders(), "a fully filled order is terminal and no longer open")
	assert.True(t, snap.Equity().Equal(usd("10000")), "cash/equity effects of a fill are issue #152's scope, not this package's yet")

	require.Len(t, snap.Positions(), 1)
	pos := snap.Positions()[0]
	assert.Equal(t, order.Long, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("1000")))
	require.NotNil(t, pos.AvgPrice)
	assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.10000")))

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	events := drainEvents(t, reader, 3)

	require.Equal(t, brokerpkg.EventKindOrder, events[0].Kind)
	require.NotNil(t, events[0].Order)
	assert.Equal(t, order.StatusWorking, events[0].Order.Status)

	require.Equal(t, brokerpkg.EventKindFill, events[1].Kind)
	require.NotNil(t, events[1].Fill)
	assert.True(t, events[1].Fill.Price.Equal(num.MustParsePrice("1.10000")))
	assert.True(t, events[1].Fill.Quantity.Equal(num.MustParseQuantity("1000")))
	assert.Equal(t, events[0].Metadata.EventID, events[1].Metadata.CausationID, "the fill is caused by the order-accepted event")

	require.Equal(t, brokerpkg.EventKindOrder, events[2].Kind)
	require.NotNil(t, events[2].Order)
	assert.Equal(t, order.StatusFilled, events[2].Order.Status)
	assert.Equal(t, events[1].Metadata.EventID, events[2].Metadata.CausationID, "the filled-status event is caused by the fill event")

	assert.Less(t, events[0].Sequence, events[1].Sequence)
	assert.Less(t, events[1].Sequence, events[2].Sequence)
}

func TestAccountSubmitMarketSellOpensShortPosition(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Sell, "1000")
	o, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, o.Status)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.True(t, snap.Equity().Equal(usd("10000")), "cash/equity effects of a fill are issue #152's scope, not this package's yet")

	require.Len(t, snap.Positions(), 1)
	pos := snap.Positions()[0]
	assert.Equal(t, order.Short, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("1000")))
}

func TestAccountSubmitMarketOrderIsIdempotentAfterFill(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	first, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	second, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, second.Status)
	assert.True(t, first.FilledQuantity.Equal(second.FilledQuantity))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.True(t, snap.Equity().Equal(usd("10000")), "cash/equity effects of a fill are issue #152's scope, not this package's yet")
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].Quantity.Equal(num.MustParseQuantity("1000")), "resubmission must not double the position")

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	events := drainEvents(t, reader, 3)
	assertNoMoreEventsSoon(t, reader) // resubmission must not emit a fourth event
	assert.Len(t, events, 3)
}

// TestAccountSubmitMarketOrderIncreasesExistingPosition covers issue
// #152's (M3-09) "increase" transition: a second same-side market fill
// against an already-open position adds to it and recomputes the
// quantity-weighted average price (ADR-027), rather than being
// rejected the way #149/#150 originally left it.
func TestAccountSubmitMarketOrderIncreasesExistingPosition(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	first := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, first)
	require.NoError(t, err)

	second := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "500")
	_, err = acc.Submit(ctx, second)
	require.NoError(t, err)

	// Both fills are at the fixed price source's single EUR_USD price
	// (1.10000), so the weighted average is trivially still 1.10000,
	// and equity is unchanged (mark == avg, no realized PnL from an
	// increase).
	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.True(t, snap.Equity().Equal(usd("10000")))
	assert.True(t, snap.RealizedPnL().Equal(usd("0")))
	require.Len(t, snap.Positions(), 1)
	pos := snap.Positions()[0]
	assert.Equal(t, order.Long, pos.Side)
	assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("1500")))
	require.NotNil(t, pos.AvgPrice)
	assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.10000")))
	assert.Empty(t, snap.OpenOrders())

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	drainEvents(t, reader, 6) // three events per market order fill
	assertNoMoreEventsSoon(t, reader)
}

func TestAccountSubmitMarketOrderPriceSourceErrorLeavesNoState(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	deps.Prices = fixedPriceSource{} // no price configured for any listing
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.Error(t, err)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.True(t, snap.Equity().Equal(usd("10000")))
	assert.Empty(t, snap.OpenOrders())
	assert.Empty(t, snap.Positions())

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	assertNoMoreEventsSoon(t, reader) // not even the order-accepted event survives a failed fill
}

func TestMarketOrderFillsAreIsolatedAcrossAccounts(t *testing.T) {
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

	req := mustMarketRequest(t, deps.IDs, a1, order.Buy, "1000")
	_, err = acc1.Submit(ctx, req)
	require.NoError(t, err)

	snap1, err := acc1.Snapshot(ctx)
	require.NoError(t, err)
	snap2, err := acc2.Snapshot(ctx)
	require.NoError(t, err)

	assert.True(t, snap1.Equity().Equal(usd("10000")), "cash/equity effects of a fill are issue #152's scope, not this package's yet")
	assert.Len(t, snap1.Positions(), 1)
	assert.True(t, snap2.Equity().Equal(usd("5000")), "account 1's fill must not affect account 2's cash")
	assert.Empty(t, snap2.Positions(), "account 1's fill must not affect account 2's positions")
}

// TestSnapshotPositionsOrderingIsDeterministic opens positions on two
// different listings and confirms repeated Snapshot calls always
// report them in the same order. accountState.positions is a map,
// exactly the same determinism hazard snapshotLocked's OpenOrders
// sorting already guards against.
func TestSnapshotPositionsOrderingIsDeterministic(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("100000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	eurUsd := mustMarketRequestFor(t, deps.IDs, accountID, mustEurUsdListing(t), order.Buy, "1000")
	_, err = acc.Submit(ctx, eurUsd)
	require.NoError(t, err)
	gbpUsd := mustMarketRequestFor(t, deps.IDs, accountID, mustGbpUsdListing(t), order.Sell, "500")
	_, err = acc.Submit(ctx, gbpUsd)
	require.NoError(t, err)

	want := []instrument.ID{eurUsd.Listing.InstrumentID(), gbpUsd.Listing.InstrumentID()}
	sort.Slice(want, func(i, j int) bool { return want[i].String() < want[j].String() })

	for range 5 {
		snap, err := acc.Snapshot(ctx)
		require.NoError(t, err)
		require.Len(t, snap.Positions(), 2)
		got := []instrument.ID{snap.Positions()[0].Listing.InstrumentID(), snap.Positions()[1].Listing.InstrumentID()}
		assert.Equal(t, want, got, "Positions order must be identical across repeated Snapshot calls")
	}
}

// TestSnapshotOpenOrdersOrderingIsDeterministic submits several orders
// and confirms repeated Snapshot calls always report them in the same
// order. accountState.orders is a map; without sorting before
// building the Snapshot, Go's randomized map iteration would let two
// otherwise-identical runs disagree on OpenOrders order, breaking this
// package's determinism promise.
func TestSnapshotOpenOrdersOrderingIsDeterministic(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	const numOrders = 8
	var orderIDs []id.OrderID
	for range numOrders {
		req := mustRequest(t, deps.IDs, accountID)
		_, err := acc.Submit(ctx, req)
		require.NoError(t, err)
		orderIDs = append(orderIDs, req.OrderID)
	}

	var want []string
	for _, oid := range orderIDs {
		want = append(want, oid.String())
	}
	sort.Strings(want)

	for range 5 {
		snap, err := acc.Snapshot(ctx)
		require.NoError(t, err)
		require.Len(t, snap.OpenOrders(), numOrders)

		var got []string
		for _, o := range snap.OpenOrders() {
			got = append(got, o.Request.OrderID.String())
		}
		assert.Equal(t, want, got, "OpenOrders order must be identical across repeated Snapshot calls")
	}
}

// failingEntropySource is an id.EntropySource that always fails,
// letting tests force id.GenerateEventID (and therefore
// accountState.buildOrderEvent) to fail deterministically.
type failingEntropySource struct{}

func (failingEntropySource) Entropy() ([10]byte, error) {
	return [10]byte{}, errors.New("sim_test: injected entropy failure")
}

// TestAccountSubmitLeavesNoStateWhenEventGenerationFails is the
// regression for the atomicity issue found in review: if
// buildOrderEvent fails (here, forced via a failing EntropySource),
// Submit must leave the account's state completely untouched — no
// order accepted into OpenOrders, no event appended, and sequence
// unchanged — so a retried Submit for the same OrderID starts fresh
// rather than idempotently "succeeding" with an order that was never
// actually recorded with a matching event.
func TestAccountSubmitLeavesNoStateWhenEventGenerationFails(t *testing.T) {
	ctx := context.Background()
	c := clock.NewSimulated(testStart)
	failingIDs := id.NewGenerator(c, failingEntropySource{})
	deps := Deps{Clock: c, IDs: failingIDs, Prices: fixedPriceSource{"EUR_USD": num.MustParsePrice("1.10000")}}

	workingGen := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID := mustAccountID(t, workingGen)

	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{}, // EventID left zero: does not require the failing generator
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t, workingGen))
	require.NoError(t, err)

	_, err = acc.Submit(ctx, req)
	require.Error(t, err, "Submit must fail when the injected id.Generator cannot produce an EventID")

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.OpenOrders(), "a failed event build must leave no order accepted into account state")

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	assertNoMoreEventsSoon(t, reader) // a failed event build must append no event
}

// Cancel/Replace behavior (issue #151, M3-08) is covered in
// cancel_replace_test.go.

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
	events := drainEvents(t, reader, 2)
	assert.Equal(t, uint64(1), events[0].Sequence)
	assert.Equal(t, uint64(2), events[1].Sequence)
	assert.Equal(t, req1.OrderID, events[0].Order.Request.OrderID)
	assert.Equal(t, req2.OrderID, events[1].Order.Request.OrderID)

	reader2, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader2.Close() }()
	replay := drainEvents(t, reader2, 2)
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
	first := drainEvents(t, reader, 1)
	_ = reader.Close()

	req2 := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req2)
	require.NoError(t, err)

	cursor := encodeCursor(first[len(first)-1].Sequence)
	resumed, err := acc.Events(ctx, cursor)
	require.NoError(t, err)
	defer func() { _ = resumed.Close() }()
	second := drainEvents(t, resumed, 1)
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

// TestAccountEventsNextRespectsContextCanceledWhileBlockedOnMutex holds
// the reader's own mutex externally, with no event yet recorded, so
// Next must genuinely block trying to acquire it; canceling ctx while
// blocked there must still surface as ctx.Err() once Next proceeds —
// not a stale success — whether Next notices the cancellation while
// still contending for the mutex or only once it reaches its final
// blocking select.
func TestAccountEventsNextRespectsContextCanceledWhileBlockedOnMutex(t *testing.T) {
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

// TestAccountEventsNextBlocksUntilLaterSubmit is the regression for the
// ADR-024 contract Next must honor: it must not return io.EOF merely
// because it has caught up. It opens a reader against an account with
// no events yet, starts Next in a goroutine, confirms Next has not
// returned after a short wait, then Submits and confirms Next delivers
// exactly that event rather than having already given up.
func TestAccountEventsNextBlocksUntilLaterSubmit(t *testing.T) {
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

	result := make(chan struct {
		event brokerpkg.Event
		err   error
	}, 1)
	go func() {
		e, err := reader.Next(ctx)
		result <- struct {
			event brokerpkg.Event
			err   error
		}{e, err}
	}()

	select {
	case <-result:
		t.Fatal("Next returned before any event existed; it must block, not return io.EOF, per ADR-024")
	case <-time.After(50 * time.Millisecond):
	}

	req := mustRequest(t, deps.IDs, accountID)
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	select {
	case r := <-result:
		require.NoError(t, r.err)
		assert.Equal(t, uint64(1), r.event.Sequence)
		assert.Equal(t, req.OrderID, r.event.Order.Request.OrderID)
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not deliver the event Submit produced")
	}
}

// TestAccountEventsNextReturnsEOFAfterBrokerClose confirms io.EOF is
// reserved for the producer actually ending (Broker.Close), including
// waking a reader that is already blocked in Next at the moment Close
// is called.
func TestAccountEventsNextReturnsEOFAfterBrokerClose(t *testing.T) {
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

	done := make(chan error, 1)
	go func() {
		_, err := reader.Next(ctx)
		done <- err
	}()

	// Give Next a moment to actually reach its blocking select before
	// closing, so this exercises "wakes an already-blocked reader," not
	// merely "a reader that starts after Close sees closed immediately."
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, b.Close())

	select {
	case err := <-done:
		require.ErrorIs(t, err, io.EOF)
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not wake up after Broker.Close")
	}
}

// TestAccountEventsNextReturnedEventDoesNotAliasStoredLog confirms
// mutating an Event Next returns cannot corrupt the account's own
// recorded event log — the aliasing guarantee Copilot's review flagged
// as missing.
func TestAccountEventsNextReturnedEventDoesNotAliasStoredLog(t *testing.T) {
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
	events := drainEvents(t, reader, 1)
	require.NotNil(t, events[0].Order.AcceptedQuantity)

	mutated := num.MustParseQuantity("999999")
	*events[0].Order.AcceptedQuantity = mutated

	reader2, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader2.Close() }()
	replay := drainEvents(t, reader2, 1)
	require.NotNil(t, replay[0].Order.AcceptedQuantity)
	assert.True(t, replay[0].Order.AcceptedQuantity.Equal(req.Quantity),
		"mutating a previously delivered Event's Order must not affect what a fresh reader observes")
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

func TestCloneEventClonesFillCommission(t *testing.T) {
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	listing := mustEurUsdListing(t)
	commission := usd("1.50")

	fill, err := order.NewFill(order.Fill{
		FillID:     mustFillID(t, deps.IDs),
		OrderID:    mustOrderID(t, deps.IDs),
		AccountID:  accountID,
		Listing:    listing,
		Side:       order.Buy,
		Price:      num.MustParsePrice("1.10000"),
		Quantity:   num.MustParseQuantity("1000"),
		Commission: &commission,
	})
	require.NoError(t, err)

	original, err := brokerpkg.NewEvent(brokerpkg.Event{
		Metadata:   id.Metadata{EventID: mustEventID(t, deps.IDs), Timestamp: testStart},
		ObservedAt: testStart,
		Sequence:   1,
		Kind:       brokerpkg.EventKindFill,
		Fill:       &fill,
	})
	require.NoError(t, err)

	clone := cloneEvent(original)
	require.NotNil(t, clone.Fill.Commission)
	assert.True(t, clone.Fill.Commission.Equal(commission))

	mutated := usd("999.99")
	*clone.Fill.Commission = mutated
	assert.True(t, original.Fill.Commission.Equal(commission),
		"mutating the clone's Commission must not affect the original event's Fill")
}

func TestCloneEventClonesStatus(t *testing.T) {
	deps := testDeps()
	status := brokerpkg.Status{State: brokerpkg.AccountStatusActive, BrokerCode: "OK", Message: "connected"}

	original, err := brokerpkg.NewEvent(brokerpkg.Event{
		Metadata:   id.Metadata{EventID: mustEventID(t, deps.IDs), Timestamp: testStart},
		ObservedAt: testStart,
		Sequence:   1,
		Kind:       brokerpkg.EventKindStatus,
		Status:     &status,
	})
	require.NoError(t, err)

	clone := cloneEvent(original)
	require.NotNil(t, clone.Status)
	assert.Equal(t, *original.Status, *clone.Status)

	clone.Status.Message = "mutated"
	assert.Equal(t, "connected", original.Status.Message,
		"mutating the clone's Status must not affect the original event's Status")
}

func mustFillID(t *testing.T, gen *id.Generator) id.FillID {
	t.Helper()
	fid, err := id.GenerateFillID(gen)
	require.NoError(t, err)
	return fid
}

func TestDecodeCursorMalformedStringDefaultsToZero(t *testing.T) {
	assert.Equal(t, uint64(0), decodeCursor(brokerpkg.EventCursor("not-a-number")))
	assert.Equal(t, uint64(0), decodeCursor(brokerpkg.EventCursor("")))
	assert.Equal(t, uint64(3), decodeCursor(encodeCursor(3)))
}
