package broker_test

import (
	"context"
	"io"
	"strconv"
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
	snapshot     account.Snapshot
	orders       map[id.OrderID]order.Order
	events       []broker.Event
	nextSequence uint64
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
	if err := a.appendOrderEvent(o, req.Metadata.EventID); err != nil {
		return order.Order{}, err
	}
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
		return order.CancelResult{}, broker.ErrOrderNotFound
	}

	pending, err := order.ApplyCancelRequest(o, req)
	if err != nil {
		return order.CancelResult{}, err
	}

	resultEventID, err := id.GenerateEventID(testGenerator)
	if err != nil {
		return order.CancelResult{}, err
	}
	result, err := order.NewCancelResult(order.CancelResult{
		OrderID:  req.OrderID,
		Status:   order.StatusCanceled,
		Metadata: id.Metadata{EventID: resultEventID, CausationID: req.Metadata.EventID},
	})
	if err != nil {
		return order.CancelResult{}, err
	}

	final, err := order.ApplyCancelResult(pending, result)
	if err != nil {
		return order.CancelResult{}, err
	}
	a.state.orders[req.OrderID] = final
	if err := a.appendOrderEvent(final, req.Metadata.EventID); err != nil {
		return order.CancelResult{}, err
	}
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
		return order.ReplaceResult{}, broker.ErrOrderNotFound
	}

	pending, err := order.ApplyReplaceRequest(o, req)
	if err != nil {
		return order.ReplaceResult{}, err
	}

	resultEventID, err := id.GenerateEventID(testGenerator)
	if err != nil {
		return order.ReplaceResult{}, err
	}
	result, err := order.NewReplaceResult(order.ReplaceResult{
		OrderID:  req.OrderID,
		Status:   order.StatusWorking,
		Metadata: id.Metadata{EventID: resultEventID, CausationID: req.Metadata.EventID},
	})
	if err != nil {
		return order.ReplaceResult{}, err
	}

	final, err := order.ApplyReplaceResult(pending, req, result)
	if err != nil {
		return order.ReplaceResult{}, err
	}
	a.state.orders[req.OrderID] = final
	if err := a.appendOrderEvent(final, req.Metadata.EventID); err != nil {
		return order.ReplaceResult{}, err
	}
	return result, nil
}

// appendOrderEvent records o as an EventKindOrder Event caused by
// causationID, assigning the next Sequence in this account's event
// stream. Callers must already hold a.broker.mu.
func (a *fakeAccount) appendOrderEvent(o order.Order, causationID id.EventID) error {
	eventID, err := id.GenerateEventID(testGenerator)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	a.state.nextSequence++
	ev, err := broker.NewEvent(broker.Event{
		Metadata: id.Metadata{
			EventID:     eventID,
			CausationID: causationID,
			Timestamp:   now,
		},
		ObservedAt: now,
		Sequence:   a.state.nextSequence,
		Kind:       broker.EventKindOrder,
		Order:      &o,
	})
	if err != nil {
		return err
	}
	a.state.events = append(a.state.events, ev)
	return nil
}

// Events returns a fakeEventReader over a.state.events, replaying
// events with Sequence greater than cursor decodes to.
func (a *fakeAccount) Events(ctx context.Context, cursor broker.EventCursor) (broker.EventReader, error) {
	a.broker.mu.Lock()
	defer a.broker.mu.Unlock()
	if a.broker.closed {
		return nil, broker.ErrClosed
	}
	return &fakeEventReader{account: a, after: decodeCursor(cursor)}, nil
}

// fakeEventReader is a minimal broker.EventReader over a fakeAccount's
// in-memory event log. It is bounded/replay-only — Next returns io.EOF
// once every currently recorded event has been delivered, matching the
// "finished backtest run" case EventReader.Next's doc comment
// describes, rather than blocking for events that do not yet exist.
type fakeEventReader struct {
	account *fakeAccount
	after   uint64
	idx     int
	closed  bool
}

var _ broker.EventReader = (*fakeEventReader)(nil)

func (r *fakeEventReader) Next(ctx context.Context) (broker.Event, error) {
	select {
	case <-ctx.Done():
		return broker.Event{}, ctx.Err()
	default:
	}

	r.account.broker.mu.Lock()
	defer r.account.broker.mu.Unlock()

	// Re-check ctx after acquiring the mutex: the pre-lock check above
	// only catches cancellation that happened before Next was called.
	// Without this second check, Next could block on a contended mutex
	// past the point ctx was canceled and then report io.EOF or a stale
	// event instead of ctx.Err() once it finally acquires the lock.
	select {
	case <-ctx.Done():
		return broker.Event{}, ctx.Err()
	default:
	}

	if r.closed {
		return broker.Event{}, broker.ErrClosed
	}

	events := r.account.state.events
	for r.idx < len(events) {
		e := events[r.idx]
		r.idx++
		if e.Sequence > r.after {
			return e, nil
		}
	}
	return broker.Event{}, io.EOF
}

func (r *fakeEventReader) Close() error {
	r.account.broker.mu.Lock()
	defer r.account.broker.mu.Unlock()
	r.closed = true
	return nil
}

// encodeCursor and decodeCursor let the fake resume a stream by
// encoding/decoding Sequence into broker.EventCursor's opaque string
// representation. A real adapter's own cursor encoding is its own
// concern; broker.EventCursor promises callers nothing about format.
func encodeCursor(seq uint64) broker.EventCursor {
	return broker.EventCursor(strconv.FormatUint(seq, 10))
}

func decodeCursor(c broker.EventCursor) uint64 {
	if c == "" {
		return 0
	}
	seq, err := strconv.ParseUint(string(c), 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

// testGenerator is shared across this file's tests for the same reason
// order/account's own test helpers share one: a Deterministic entropy
// source paired with a clock that never advances only produces distinct
// values across successive calls on the same Generator.
var testGenerator = id.NewGenerator(clock.NewSimulated(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))
