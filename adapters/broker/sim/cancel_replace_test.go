package sim

import (
	"context"
	"testing"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustCancelRequest(t *testing.T, gen *id.Generator, orderID id.OrderID) order.CancelRequest {
	t.Helper()
	req, err := order.NewCancelRequest(order.CancelRequest{OrderID: orderID, Metadata: id.Metadata{EventID: mustEventID(t, gen)}})
	require.NoError(t, err)
	return req
}

func mustReplaceRequest(t *testing.T, gen *id.Generator, orderID id.OrderID, newQuantity string) order.ReplaceRequest {
	t.Helper()
	qty := num.MustParseQuantity(newQuantity)
	req, err := order.NewReplaceRequest(order.ReplaceRequest{OrderID: orderID, NewQuantity: &qty, Metadata: id.Metadata{EventID: mustEventID(t, gen)}})
	require.NoError(t, err)
	return req
}

// TestCancelWorkingOrderSucceeds covers cancel-before-fill.
func TestCancelWorkingOrderSucceeds(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, deps.IDs, accountID) // Limit order, stays Working
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	cancelReq := mustCancelRequest(t, deps.IDs, req.OrderID)
	result, err := acc.Cancel(ctx, cancelReq)
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, result.Status)
	assert.Nil(t, result.Rejection)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.OpenOrders(), "a canceled order is terminal and no longer open")

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	events := drainEvents(t, reader, 3) // accept, pending-cancel, canceled
	assertNoMoreEventsSoon(t, reader)

	require.Equal(t, brokerpkg.EventKindOrder, events[0].Kind)
	assert.Equal(t, order.StatusWorking, events[0].Order.Status)

	require.Equal(t, brokerpkg.EventKindOrder, events[1].Kind)
	assert.Equal(t, order.StatusPendingCancel, events[1].Order.Status)
	assert.Equal(t, cancelReq.Metadata.EventID, events[1].Metadata.CausationID, "the CancelRequest's own EventID causes the pending-cancel transition")

	require.Equal(t, brokerpkg.EventKindOrder, events[2].Kind)
	assert.Equal(t, order.StatusCanceled, events[2].Order.Status)
	assert.Equal(t, events[1].Metadata.EventID, events[2].Metadata.CausationID, "the pending-cancel event causes the final canceled transition")
}

// TestCancelAlreadyFilledOrderIsDeclined covers cancel-after-fill, and
// the cancel/fill race scenario: by the time Cancel is called, the
// order has already terminally filled.
func TestCancelAlreadyFilledOrderIsDeclined(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	cancelReq := mustCancelRequest(t, deps.IDs, req.OrderID)
	result, err := acc.Cancel(ctx, cancelReq)
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, result.Status)
	require.NotNil(t, result.Rejection)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	drainEvents(t, reader, 3) // exactly the market order's own three events
	assertNoMoreEventsSoon(t, reader)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1, "the declined cancel must not affect the already-filled order's position")
}

func TestCancelAlreadyCanceledOrderIsIdempotent(t *testing.T) {
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

	first, err := acc.Cancel(ctx, mustCancelRequest(t, deps.IDs, req.OrderID))
	require.NoError(t, err)
	require.Equal(t, order.StatusCanceled, first.Status)

	second, err := acc.Cancel(ctx, mustCancelRequest(t, deps.IDs, req.OrderID))
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, second.Status)
	assert.Nil(t, second.Rejection)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	drainEvents(t, reader, 3) // accept, pending-cancel, canceled from the first Cancel only
	assertNoMoreEventsSoon(t, reader)
}

func TestCancelUnknownOrderReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	unknownOrderID := mustOrderID(t, deps.IDs)
	_, err = acc.Cancel(ctx, mustCancelRequest(t, deps.IDs, unknownOrderID))
	require.ErrorIs(t, err, brokerpkg.ErrOrderNotFound)
}

func TestCancelRejectsInvalidRequest(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	_, err = acc.Cancel(ctx, order.CancelRequest{}) // zero OrderID
	require.Error(t, err)
}

func TestCancelOnClosedBrokerReturnsErrClosed(t *testing.T) {
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
	require.NoError(t, b.Close())

	_, err = acc.Cancel(ctx, mustCancelRequest(t, deps.IDs, req.OrderID))
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

// TestReplaceWorkingOrderQuantitySucceeds covers replace-before-fill.
func TestReplaceWorkingOrderQuantitySucceeds(t *testing.T) {
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

	replaceReq := mustReplaceRequest(t, deps.IDs, req.OrderID, "500")
	result, err := acc.Replace(ctx, replaceReq)
	require.NoError(t, err)
	assert.Equal(t, order.StatusWorking, result.Status)
	assert.Nil(t, result.Rejection)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1)
	require.NotNil(t, snap.OpenOrders()[0].AcceptedQuantity)
	assert.True(t, snap.OpenOrders()[0].AcceptedQuantity.Equal(num.MustParseQuantity("500")))

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	events := drainEvents(t, reader, 3) // accept, pending-replace, replaced
	assertNoMoreEventsSoon(t, reader)

	require.Equal(t, order.StatusPendingReplace, events[1].Order.Status)
	assert.Equal(t, replaceReq.Metadata.EventID, events[1].Metadata.CausationID, "the ReplaceRequest's own EventID causes the pending-replace transition")
	require.Equal(t, order.StatusWorking, events[2].Order.Status)
	assert.Equal(t, events[1].Metadata.EventID, events[2].Metadata.CausationID)
	require.NotNil(t, events[2].Order.AcceptedQuantity)
	assert.True(t, events[2].Order.AcceptedQuantity.Equal(num.MustParseQuantity("500")))
}

// TestReplaceInvalidQuantityIsDeclined covers the invalid-replacement
// acceptance criterion: a new quantity that violates the listing's
// quantity increment is declined, and the order's original accepted
// quantity is left completely unchanged.
func TestReplaceInvalidQuantityIsDeclined(t *testing.T) {
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

	// mustEurUsdListing's quantity increment is 1; 500.5 violates it.
	replaceReq := mustReplaceRequest(t, deps.IDs, req.OrderID, "500.5")
	result, err := acc.Replace(ctx, replaceReq)
	require.NoError(t, err)
	assert.Equal(t, order.StatusWorking, result.Status)
	require.NotNil(t, result.Rejection)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1)
	require.NotNil(t, snap.OpenOrders()[0].AcceptedQuantity)
	assert.True(t, snap.OpenOrders()[0].AcceptedQuantity.Equal(req.Quantity), "a declined replace must leave the order's accepted quantity unchanged")

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	events := drainEvents(t, reader, 3)
	assertNoMoreEventsSoon(t, reader)
	require.Equal(t, order.StatusWorking, events[2].Order.Status, "a declined replace still reports the resolved (unchanged) status via its own event")
}

func TestReplaceAlreadyFilledOrderIsDeclined(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	replaceReq := mustReplaceRequest(t, deps.IDs, req.OrderID, "500")
	result, err := acc.Replace(ctx, replaceReq)
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, result.Status)
	require.NotNil(t, result.Rejection)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	drainEvents(t, reader, 3) // exactly the market order's own three events
	assertNoMoreEventsSoon(t, reader)
}

func TestReplaceUnknownOrderReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	unknownOrderID := mustOrderID(t, deps.IDs)
	_, err = acc.Replace(ctx, mustReplaceRequest(t, deps.IDs, unknownOrderID, "500"))
	require.ErrorIs(t, err, brokerpkg.ErrOrderNotFound)
}

func TestReplaceRejectsInvalidRequest(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	_, err = acc.Replace(ctx, order.ReplaceRequest{}) // zero OrderID, no new values
	require.Error(t, err)
}

func TestReplaceOnClosedBrokerReturnsErrClosed(t *testing.T) {
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
	require.NoError(t, b.Close())

	_, err = acc.Replace(ctx, mustReplaceRequest(t, deps.IDs, req.OrderID, "500"))
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

// TestCancelDoesNotAffectAdvanceOfOtherPendingOrders is a race/order-
// of-events case: canceling one working order must not disturb a
// different pending order on the same account/listing, and a
// subsequently canceled order must never be evaluated by a later
// Advance call.
func TestCancelDoesNotAffectAdvanceOfOtherPendingOrders(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	toCancel := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, toCancel)
	require.NoError(t, err)
	toKeep := mustLimitRequest(t, deps.IDs, accountID, order.Sell, "500", "1.20000")
	_, err = acc.Submit(ctx, toKeep)
	require.NoError(t, err)

	_, err = acc.Cancel(ctx, mustCancelRequest(t, deps.IDs, toCancel.OrderID))
	require.NoError(t, err)

	// This observation would have triggered toCancel (Low reaches
	// 1.10000) had it not been canceled first.
	obs := mustObservation(t, mustEurUsdListing(t), "1.10500", "1.10600", "1.09900", "1.10300", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions(), "the canceled order must never be evaluated by Advance")
	require.Len(t, snap.OpenOrders(), 1)
	assert.Equal(t, toKeep.OrderID, snap.OpenOrders()[0].Request.OrderID)
}
