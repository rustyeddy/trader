package broker_test

import (
	"context"
	"sync"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
)

// fakeBroker and fakeAccount are minimal in-memory implementations of
// broker.Broker and broker.Account, used only to prove the interfaces
// are implementable with canonical account/order values and to
// exercise their method signatures. They are not the deterministic
// simulated broker ADR-008 and issue #148 (M3-05) build — no fill
// model, no risk of partial fills, and only enough lifecycle behavior
// to make Submit/Cancel/Replace observable.
type fakeBroker struct {
	name string

	mu       sync.Mutex
	closed   bool
	accounts map[id.AccountID]*fakeAccountState
}

type fakeAccountState struct {
	snapshot account.Snapshot
	orders   map[id.OrderID]order.Order
}

var _ broker.Broker = (*fakeBroker)(nil)

func newFakeBroker(name string, snapshots ...account.Snapshot) *fakeBroker {
	accounts := make(map[id.AccountID]*fakeAccountState, len(snapshots))
	for _, s := range snapshots {
		accounts[s.AccountID()] = &fakeAccountState{
			snapshot: s,
			orders:   make(map[id.OrderID]order.Order),
		}
	}
	return &fakeBroker{name: name, accounts: accounts}
}

func (b *fakeBroker) Name() string { return b.name }

func (b *fakeBroker) Accounts(ctx context.Context) ([]account.Reference, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, broker.ErrClosed
	}

	refs := make([]account.Reference, 0, len(b.accounts))
	for accountID := range b.accounts {
		ref, err := account.NewReference(account.Reference{AccountID: accountID, Broker: b.name})
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (b *fakeBroker) OpenAccount(ctx context.Context, accountID id.AccountID) (broker.Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, broker.ErrClosed
	}

	state, ok := b.accounts[accountID]
	if !ok {
		return nil, broker.ErrAccountNotFound
	}
	return &fakeAccount{broker: b, accountID: accountID, state: state}, nil
}

func (b *fakeBroker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

// fakeAccount is broker.Account bound to one account within a
// fakeBroker. Submit immediately accepts the request into
// StatusWorking; Cancel and Replace operate synchronously against the
// in-memory order map, since this fake has no asynchronous broker to
// wait on.
type fakeAccount struct {
	broker    *fakeBroker
	accountID id.AccountID
	state     *fakeAccountState
}

var _ broker.Account = (*fakeAccount)(nil)

func (a *fakeAccount) Reference() account.Reference {
	ref, err := account.NewReference(account.Reference{AccountID: a.accountID, Broker: a.broker.name})
	if err != nil {
		// a.accountID and a.broker.name were already validated when
		// this fakeAccount was constructed via OpenAccount.
		panic(err)
	}
	return ref
}

func (a *fakeAccount) Snapshot(ctx context.Context) (account.Snapshot, error) {
	a.broker.mu.Lock()
	defer a.broker.mu.Unlock()
	if a.broker.closed {
		return account.Snapshot{}, broker.ErrClosed
	}
	return a.state.snapshot, nil
}

func (a *fakeAccount) Submit(ctx context.Context, req order.Request) (order.Order, error) {
	a.broker.mu.Lock()
	defer a.broker.mu.Unlock()
	if a.broker.closed {
		return order.Order{}, broker.ErrClosed
	}

	accepted := req.Quantity
	o, err := order.NewOrder(order.Order{
		Request:          req,
		BrokerOrderID:    "fake-" + req.OrderID.String(),
		AcceptedQuantity: &accepted,
		Status:           order.StatusWorking,
		UpdatedAt:        time.Now().UTC(),
	})
	if err != nil {
		return order.Order{}, err
	}
	a.state.orders[req.OrderID] = o
	return o, nil
}

func (a *fakeAccount) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	a.broker.mu.Lock()
	defer a.broker.mu.Unlock()
	if a.broker.closed {
		return order.CancelResult{}, broker.ErrClosed
	}

	o, ok := a.state.orders[req.OrderID]
	if !ok {
		return order.CancelResult{}, broker.ErrAccountNotFound
	}

	pending, err := order.ApplyCancelRequest(o, req)
	if err != nil {
		return order.CancelResult{}, err
	}

	result, err := order.NewCancelResult(order.CancelResult{
		OrderID:  req.OrderID,
		Status:   order.StatusCanceled,
		Metadata: id.Metadata{EventID: req.Metadata.EventID, CausationID: req.Metadata.EventID},
	})
	if err != nil {
		return order.CancelResult{}, err
	}

	final, err := order.ApplyCancelResult(pending, result)
	if err != nil {
		return order.CancelResult{}, err
	}
	a.state.orders[req.OrderID] = final
	return result, nil
}

func (a *fakeAccount) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	a.broker.mu.Lock()
	defer a.broker.mu.Unlock()
	if a.broker.closed {
		return order.ReplaceResult{}, broker.ErrClosed
	}

	o, ok := a.state.orders[req.OrderID]
	if !ok {
		return order.ReplaceResult{}, broker.ErrAccountNotFound
	}

	pending, err := order.ApplyReplaceRequest(o, req)
	if err != nil {
		return order.ReplaceResult{}, err
	}

	result, err := order.NewReplaceResult(order.ReplaceResult{
		OrderID:  req.OrderID,
		Status:   order.StatusWorking,
		Metadata: id.Metadata{EventID: req.Metadata.EventID, CausationID: req.Metadata.EventID},
	})
	if err != nil {
		return order.ReplaceResult{}, err
	}

	final, err := order.ApplyReplaceResult(pending, req, result)
	if err != nil {
		return order.ReplaceResult{}, err
	}
	a.state.orders[req.OrderID] = final
	return result, nil
}

// testGenerator is shared across this file's tests for the same reason
// order/account's own test helpers share one: a Deterministic entropy
// source paired with a clock that never advances only produces distinct
// values across successive calls on the same Generator.
var testGenerator = id.NewGenerator(clock.NewSimulated(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))
