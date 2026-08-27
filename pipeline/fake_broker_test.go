package pipeline_test

import (
	"context"
	"errors"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
)

// failingBroker is a minimal brokerpkg.Broker test double whose
// OpenAccount/Submit errors are fully controlled by its fields, used
// only to exercise Pipeline.Submit's own error-wrapping around those
// two calls — a real sim.Broker has no way to fail either
// deterministically for a structurally valid request.
type failingBroker struct {
	name      string
	openErr   error
	submitErr error
}

func (b failingBroker) Name() string {
	if b.name == "" {
		return "sim"
	}
	return b.name
}

func (b failingBroker) Accounts(ctx context.Context) ([]account.Reference, error) {
	return nil, nil
}

func (b failingBroker) OpenAccount(ctx context.Context, accountID id.AccountID) (brokerpkg.Account, error) {
	if b.openErr != nil {
		return nil, b.openErr
	}
	return failingAccount{submitErr: b.submitErr}, nil
}

func (b failingBroker) Close() error { return nil }

type failingAccount struct {
	submitErr error
}

func (a failingAccount) Reference() account.Reference { return account.Reference{} }

func (a failingAccount) Snapshot(ctx context.Context) (account.Snapshot, error) {
	return account.Snapshot{}, errors.New("not implemented")
}

func (a failingAccount) Submit(ctx context.Context, req order.Request) (order.Order, error) {
	return order.Order{}, a.submitErr
}

func (a failingAccount) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	return order.CancelResult{}, errors.New("not implemented")
}

func (a failingAccount) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	return order.ReplaceResult{}, errors.New("not implemented")
}

func (a failingAccount) Events(ctx context.Context, cursor brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	return nil, errors.New("not implemented")
}

var _ brokerpkg.Broker = failingBroker{}
var _ brokerpkg.Account = failingAccount{}
