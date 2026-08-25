package broker

import (
	"context"
	"testing"
	"time"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
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

type fixedPriceSource map[string]num.Price

func (f fixedPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "fixedPriceSource", Version: "test"}
}

func (f fixedPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	return f[listing.Symbol()], nil
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

// testBroker constructs a deterministic sim.Broker with one funded
// account, used as a real brokerpkg.Broker implementation to exercise
// this service against — the service layer is adapter-neutral, but it
// needs a concrete adapter to test against, and sim is the only one
// that exists yet.
func testBroker(t *testing.T) (brokerpkg.Broker, id.AccountID, *id.Generator) {
	t.Helper()
	c := clock.NewSimulated(testStart)
	gen := id.NewGenerator(c, id.NewDeterministic(1, 2))
	deps := simbroker.Deps{
		Clock:  c,
		IDs:    gen,
		Prices: fixedPriceSource{"EUR_USD": num.MustParsePrice("1.10000")},
	}
	accountID, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	b, err := simbroker.NewBroker("sim", deps, simbroker.AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	return b, accountID, gen
}

func mustMarketRequest(t *testing.T, gen *id.Generator, accountID id.AccountID) order.Request {
	t.Helper()
	eventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: eventID},
	})
	require.NoError(t, err)
	orderID, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	req, err := order.NewRequest(proposal, orderID)
	require.NoError(t, err)
	return req
}

func TestNewRejectsNilBroker(t *testing.T) {
	_, err := New(nil, nil)
	require.ErrorIs(t, err, ErrNilBroker)
}

func TestNewAcceptsNilLogger(t *testing.T) {
	b, _, _ := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestServiceAccounts(t *testing.T) {
	ctx := context.Background()
	b, accountID, _ := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	resp, err := svc.Accounts(ctx, AccountsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Accounts, 1)
	assert.Equal(t, accountID, resp.Accounts[0].AccountID)
}

func TestServiceSnapshot(t *testing.T) {
	ctx := context.Background()
	b, accountID, _ := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	resp, err := svc.Snapshot(ctx, SnapshotRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	require.NoError(t, err)
	assert.True(t, resp.Snapshot.Equity().Equal(usd("10000")))
}

func TestServiceSnapshotRejectsZeroAccountID(t *testing.T) {
	ctx := context.Background()
	b, _, _ := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Snapshot(ctx, SnapshotRequest{})
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestServiceSnapshotPropagatesAccountNotFound(t *testing.T) {
	ctx := context.Background()
	b, _, gen := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	unknown, err := id.GenerateAccountID(gen)
	require.NoError(t, err)

	_, err = svc.Snapshot(ctx, SnapshotRequest{AccountRequest: AccountRequest{AccountID: unknown}})
	require.ErrorIs(t, err, brokerpkg.ErrAccountNotFound)
}

func TestServiceSubmit(t *testing.T) {
	ctx := context.Background()
	b, accountID, gen := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	req := mustMarketRequest(t, gen, accountID)
	resp, err := svc.Submit(ctx, SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: req})
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, resp.Order.Status)
}

func TestServiceSubmitRejectsMismatchedAccountID(t *testing.T) {
	ctx := context.Background()
	b, accountID, gen := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	other, err := id.GenerateAccountID(gen)
	require.NoError(t, err)

	req := mustMarketRequest(t, gen, accountID)
	_, err = svc.Submit(ctx, SubmitRequest{AccountRequest: AccountRequest{AccountID: other}, Order: req})
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestServiceCancel(t *testing.T) {
	ctx := context.Background()
	b, accountID, gen := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	unknownOrder, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	cancelEventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	cancelReq, err := order.NewCancelRequest(order.CancelRequest{OrderID: unknownOrder, Metadata: id.Metadata{EventID: cancelEventID}})
	require.NoError(t, err)

	_, err = svc.Cancel(ctx, CancelRequest{AccountRequest: AccountRequest{AccountID: accountID}, Cancel: cancelReq})
	require.ErrorIs(t, err, brokerpkg.ErrOrderNotFound)
}

func TestServiceReplace(t *testing.T) {
	ctx := context.Background()
	b, accountID, gen := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	unknownOrder, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	replaceEventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	qty := num.MustParseQuantity("500")
	replaceReq, err := order.NewReplaceRequest(order.ReplaceRequest{OrderID: unknownOrder, NewQuantity: &qty, Metadata: id.Metadata{EventID: replaceEventID}})
	require.NoError(t, err)

	_, err = svc.Replace(ctx, ReplaceRequest{AccountRequest: AccountRequest{AccountID: accountID}, Replace: replaceReq})
	require.ErrorIs(t, err, brokerpkg.ErrOrderNotFound)
}

func TestServiceReplaceRejectsZeroAccountID(t *testing.T) {
	ctx := context.Background()
	b, _, gen := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	orderID, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	qty := num.MustParseQuantity("500")
	replaceReq, err := order.NewReplaceRequest(order.ReplaceRequest{OrderID: orderID, NewQuantity: &qty, Metadata: id.Metadata{EventID: eventID}})
	require.NoError(t, err)

	_, err = svc.Replace(ctx, ReplaceRequest{Replace: replaceReq})
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestServiceAccountsPropagatesBrokerError(t *testing.T) {
	ctx := context.Background()
	b, _, _ := testBroker(t)
	require.NoError(t, b.Close())
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Accounts(ctx, AccountsRequest{})
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestServiceSnapshotPropagatesOpenAccountError(t *testing.T) {
	ctx := context.Background()
	b, accountID, _ := testBroker(t)
	require.NoError(t, b.Close())
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Snapshot(ctx, SnapshotRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestServiceSubmitPropagatesOpenAccountError(t *testing.T) {
	ctx := context.Background()
	b, accountID, gen := testBroker(t)
	req := mustMarketRequest(t, gen, accountID)
	require.NoError(t, b.Close())
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Submit(ctx, SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: req})
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestServiceCancelPropagatesOpenAccountError(t *testing.T) {
	ctx := context.Background()
	b, accountID, gen := testBroker(t)
	orderID, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	cancelReq, err := order.NewCancelRequest(order.CancelRequest{OrderID: orderID, Metadata: id.Metadata{EventID: eventID}})
	require.NoError(t, err)
	require.NoError(t, b.Close())
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Cancel(ctx, CancelRequest{AccountRequest: AccountRequest{AccountID: accountID}, Cancel: cancelReq})
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestServiceReplacePropagatesOpenAccountError(t *testing.T) {
	ctx := context.Background()
	b, accountID, gen := testBroker(t)
	orderID, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	qty := num.MustParseQuantity("500")
	replaceReq, err := order.NewReplaceRequest(order.ReplaceRequest{OrderID: orderID, NewQuantity: &qty, Metadata: id.Metadata{EventID: eventID}})
	require.NoError(t, err)
	require.NoError(t, b.Close())
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Replace(ctx, ReplaceRequest{AccountRequest: AccountRequest{AccountID: accountID}, Replace: replaceReq})
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

func TestServiceCancelRejectsZeroAccountID(t *testing.T) {
	ctx := context.Background()
	b, _, gen := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	orderID, err := id.GenerateOrderID(gen)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	cancelReq, err := order.NewCancelRequest(order.CancelRequest{OrderID: orderID, Metadata: id.Metadata{EventID: eventID}})
	require.NoError(t, err)

	_, err = svc.Cancel(ctx, CancelRequest{Cancel: cancelReq})
	require.ErrorIs(t, err, ErrInvalidRequest)
}
