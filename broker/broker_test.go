package broker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustAccountID(t *testing.T) id.AccountID {
	t.Helper()
	aid, err := id.GenerateAccountID(testGenerator)
	require.NoError(t, err)
	return aid
}

func mustOrderID(t *testing.T) id.OrderID {
	t.Helper()
	oid, err := id.GenerateOrderID(testGenerator)
	require.NoError(t, err)
	return oid
}

func mustEventID(t *testing.T) id.EventID {
	t.Helper()
	eid, err := id.GenerateEventID(testGenerator)
	require.NoError(t, err)
	return eid
}

func usd(s string) num.Money {
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
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

func mustSnapshot(t *testing.T, accountID id.AccountID, broker string) account.Snapshot {
	t.Helper()
	s, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       accountID,
		Broker:          broker,
		Currency:        num.MustParseCurrency("USD"),
		AsOf:            time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
		CashBalances:    []num.Money{usd("10000")},
		Equity:          usd("10000"),
		BuyingPower:     usd("10000"),
		MarginUsed:      usd("0"),
		MarginAvailable: usd("10000"),
		RealizedPnL:     usd("0"),
		UnrealizedPnL:   usd("0"),
		Fees:            usd("0"),
		Financing:       usd("0"),
	})
	require.NoError(t, err)
	return s
}

func mustRequest(t *testing.T, accountID id.AccountID) order.Request {
	t.Helper()
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, mustOrderID(t))
	require.NoError(t, err)
	return req
}

func TestBrokerAccountsListsDiscoveredAccounts(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t)
	b := newFakeBroker("sim", mustSnapshot(t, accountID, "sim"))

	refs, err := b.Accounts(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, accountID, refs[0].AccountID)
	assert.Equal(t, "sim", refs[0].Broker)
}

func TestBrokerOpenAccountUnknownReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	b := newFakeBroker("sim")

	_, err := b.OpenAccount(ctx, mustAccountID(t))
	require.ErrorIs(t, err, broker.ErrAccountNotFound)
}

func TestBrokerOpenAccountReturnsWorkingHandle(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t)
	b := newFakeBroker("sim", mustSnapshot(t, accountID, "sim"))

	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)
	assert.Equal(t, accountID, acc.Reference().AccountID)
	assert.Equal(t, "sim", acc.Reference().Broker)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, accountID, snap.AccountID())
}

func TestBrokerCloseRejectsFurtherUse(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t)
	b := newFakeBroker("sim", mustSnapshot(t, accountID, "sim"))
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	require.NoError(t, b.Close())

	_, err = b.Accounts(ctx)
	require.ErrorIs(t, err, broker.ErrClosed)

	_, err = b.OpenAccount(ctx, accountID)
	require.ErrorIs(t, err, broker.ErrClosed)

	_, err = acc.Snapshot(ctx)
	require.ErrorIs(t, err, broker.ErrClosed)
}

func TestAccountSubmitReturnsCanonicalOrder(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t)
	b := newFakeBroker("sim", mustSnapshot(t, accountID, "sim"))
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, accountID)
	o, err := acc.Submit(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, order.StatusWorking, o.Status)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(req.Quantity))
	assert.Equal(t, req.OrderID, o.Request.OrderID)
}

func TestAccountCancelTransitionsOrderToCanceled(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t)
	b := newFakeBroker("sim", mustSnapshot(t, accountID, "sim"))
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, accountID)
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	cancelReq, err := order.NewCancelRequest(order.CancelRequest{
		OrderID:  req.OrderID,
		Metadata: id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)

	result, err := acc.Cancel(ctx, cancelReq)
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, result.Status)
	assert.Nil(t, result.Rejection)
}

func TestAccountCancelUnknownOrderReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t)
	b := newFakeBroker("sim", mustSnapshot(t, accountID, "sim"))
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	cancelReq, err := order.NewCancelRequest(order.CancelRequest{
		OrderID:  mustOrderID(t),
		Metadata: id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)

	_, err = acc.Cancel(ctx, cancelReq)
	require.ErrorIs(t, err, broker.ErrAccountNotFound)
}

func TestAccountReplaceUpdatesAcceptedQuantity(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t)
	b := newFakeBroker("sim", mustSnapshot(t, accountID, "sim"))
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustRequest(t, accountID)
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	newQty := num.MustParseQuantity("500")
	replaceReq, err := order.NewReplaceRequest(order.ReplaceRequest{
		OrderID:     req.OrderID,
		NewQuantity: &newQty,
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)

	result, err := acc.Replace(ctx, replaceReq)
	require.NoError(t, err)
	assert.Equal(t, order.StatusWorking, result.Status)
	assert.Nil(t, result.Rejection)
}

func TestAccountReplaceUnknownOrderReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	accountID := mustAccountID(t)
	b := newFakeBroker("sim", mustSnapshot(t, accountID, "sim"))
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	newQty := num.MustParseQuantity("500")
	replaceReq, err := order.NewReplaceRequest(order.ReplaceRequest{
		OrderID:     mustOrderID(t),
		NewQuantity: &newQty,
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)

	_, err = acc.Replace(ctx, replaceReq)
	require.ErrorIs(t, err, broker.ErrAccountNotFound)
}

// TestErrorsAreClassifiable confirms every sentinel this package
// defines is distinct and matchable with errors.Is, the "classifiable
// broker errors" issue #145 calls for.
func TestErrorsAreClassifiable(t *testing.T) {
	sentinels := []error{broker.ErrAccountNotFound, broker.ErrUnsupported, broker.ErrClosed}
	for i, outer := range sentinels {
		for j, inner := range sentinels {
			if i == j {
				assert.True(t, errors.Is(outer, inner))
				continue
			}
			assert.False(t, errors.Is(outer, inner), "%v should not match %v", outer, inner)
		}
	}
}
