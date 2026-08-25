package broker

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/require"
)

// ctxCheckingBroker and ctxCheckingAccount are a minimal brokerpkg
// .Broker/brokerpkg.Account fake whose every method's first act is to
// check ctx.Err() and return it verbatim if non-nil. This pins down
// Service's own context-propagation contract (issue #154's explicit
// "propagate context/cancellation" acceptance criterion) independent
// of whether the current adapters/broker/sim implementation happens to
// check ctx anywhere — the point is proving Service passes the exact
// caller-supplied context through unmodified, not exercising a
// particular adapter's own behavior.
type ctxCheckingBroker struct {
	ref account.Reference
}

func (b *ctxCheckingBroker) Name() string { return "ctx-checking" }

func (b *ctxCheckingBroker) Accounts(ctx context.Context) ([]account.Reference, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []account.Reference{b.ref}, nil
}

func (b *ctxCheckingBroker) OpenAccount(ctx context.Context, accountID id.AccountID) (brokerpkg.Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &ctxCheckingAccount{ref: b.ref}, nil
}

func (b *ctxCheckingBroker) Close() error { return nil }

type ctxCheckingAccount struct {
	ref account.Reference
}

func (a *ctxCheckingAccount) Reference() account.Reference { return a.ref }

func (a *ctxCheckingAccount) Snapshot(ctx context.Context) (account.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return account.Snapshot{}, err
	}
	return account.Snapshot{}, nil
}

func (a *ctxCheckingAccount) Submit(ctx context.Context, req order.Request) (order.Order, error) {
	if err := ctx.Err(); err != nil {
		return order.Order{}, err
	}
	return order.Order{}, nil
}

func (a *ctxCheckingAccount) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	if err := ctx.Err(); err != nil {
		return order.CancelResult{}, err
	}
	return order.CancelResult{}, nil
}

func (a *ctxCheckingAccount) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	if err := ctx.Err(); err != nil {
		return order.ReplaceResult{}, err
	}
	return order.ReplaceResult{}, nil
}

func (a *ctxCheckingAccount) Events(ctx context.Context, cursor brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	return nil, brokerpkg.ErrUnsupported
}

func mustCtxCheckingBroker(t *testing.T) (*ctxCheckingBroker, id.AccountID) {
	t.Helper()
	accountID, err := id.GenerateAccountID(id.NewGenerator(clock.NewSimulated(testStart), id.NewDeterministic(1, 2)))
	require.NoError(t, err)
	ref, err := account.NewReference(account.Reference{AccountID: accountID, Broker: "ctx-checking"})
	require.NoError(t, err)
	return &ctxCheckingBroker{ref: ref}, accountID
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestServiceAccountsPropagatesContextCancellation(t *testing.T) {
	b, _ := mustCtxCheckingBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Accounts(canceledContext(), AccountsRequest{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestServiceSnapshotPropagatesContextCancellation(t *testing.T) {
	b, accountID := mustCtxCheckingBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Snapshot(canceledContext(), SnapshotRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestServiceSubmitPropagatesContextCancellation(t *testing.T) {
	b, accountID := mustCtxCheckingBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	req := order.Request{Proposal: order.Proposal{AccountID: accountID}}
	_, err = svc.Submit(canceledContext(), SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: req})
	require.ErrorIs(t, err, context.Canceled)
}

func TestServiceCancelPropagatesContextCancellation(t *testing.T) {
	b, accountID := mustCtxCheckingBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Cancel(canceledContext(), CancelRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestServiceReplacePropagatesContextCancellation(t *testing.T) {
	b, accountID := mustCtxCheckingBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	_, err = svc.Replace(canceledContext(), ReplaceRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	require.ErrorIs(t, err, context.Canceled)
}
