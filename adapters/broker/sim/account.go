package sim

import (
	"context"
	"sync"
	"time"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// accountState is one simulated account's mutable state, independently
// guarded by its own mutex so operations against different accounts
// never contend. asOf tracks the last time this state actually changed
// (construction, or a Submit); Snapshot reports it directly rather than
// re-reading the clock on every query, so two Snapshot calls with no
// intervening state change report identical AsOf values.
type accountState struct {
	mu sync.Mutex

	ref      account.Reference
	currency num.Currency
	cash     num.Money
	zero     num.Money
	asOf     time.Time

	openOrders map[id.OrderID]order.Order

	events       []brokerpkg.Event
	nextSequence uint64
}

// zeroMoney returns zero money denominated in currency.
func zeroMoney(currency num.Currency) (num.Money, error) {
	return num.ParseMoney("0", currency)
}

// snapshotLocked builds this account's current account.Snapshot. The
// caller must already hold s.mu.
func (s *accountState) snapshotLocked() (account.Snapshot, error) {
	openOrders := make([]order.Order, 0, len(s.openOrders))
	for _, o := range s.openOrders {
		openOrders = append(openOrders, o)
	}

	return account.NewSnapshot(account.SnapshotParams{
		AccountID:       s.ref.AccountID,
		Broker:          s.ref.Broker,
		Currency:        s.currency,
		AsOf:            s.asOf,
		CashBalances:    []num.Money{s.cash},
		Equity:          s.cash,
		BuyingPower:     s.cash,
		MarginUsed:      s.zero,
		MarginAvailable: s.cash,
		RealizedPnL:     s.zero,
		UnrealizedPnL:   s.zero,
		Fees:            s.zero,
		Financing:       s.zero,
		OpenOrders:      openOrders,
	})
}

// appendOrderEvent records o as a deterministic EventKindOrder Event.
// The caller must already hold s.mu.
func (s *accountState) appendOrderEvent(deps Deps, o order.Order, causationID id.EventID) error {
	eventID, err := id.GenerateEventID(deps.IDs)
	if err != nil {
		return err
	}
	now := deps.Clock.Now()
	orderForEvent := cloneOrder(o)
	s.nextSequence++
	ev, err := brokerpkg.NewEvent(brokerpkg.Event{
		Metadata: id.Metadata{
			EventID:     eventID,
			CausationID: causationID,
			Timestamp:   now,
		},
		ObservedAt: now,
		Sequence:   s.nextSequence,
		Kind:       brokerpkg.EventKindOrder,
		Order:      &orderForEvent,
	})
	if err != nil {
		return err
	}
	s.events = append(s.events, ev)
	return nil
}

// cloneOrder returns a copy of o that shares no pointer or slice state
// with it, so storing or emitting the clone is safe from later mutation
// through the original — the same discipline account.Snapshot's own
// internal cloning applies, duplicated here because it is unexported
// there.
func cloneOrder(o order.Order) order.Order {
	cloned := o
	if o.Request.LimitPrice != nil {
		v := *o.Request.LimitPrice
		cloned.Request.LimitPrice = &v
	}
	if o.Request.StopPrice != nil {
		v := *o.Request.StopPrice
		cloned.Request.StopPrice = &v
	}
	if o.AcceptedQuantity != nil {
		v := *o.AcceptedQuantity
		cloned.AcceptedQuantity = &v
	}
	if o.AcceptedLimitPrice != nil {
		v := *o.AcceptedLimitPrice
		cloned.AcceptedLimitPrice = &v
	}
	if o.AcceptedStopPrice != nil {
		v := *o.AcceptedStopPrice
		cloned.AcceptedStopPrice = &v
	}
	if o.AvgFillPrice != nil {
		v := *o.AvgFillPrice
		cloned.AvgFillPrice = &v
	}
	if o.Rejection != nil {
		v := *o.Rejection
		cloned.Rejection = &v
	}
	if o.AppliedFillIDs != nil {
		cloned.AppliedFillIDs = append([]id.FillID(nil), o.AppliedFillIDs...)
	}
	if o.AppliedBrokerFillIDs != nil {
		cloned.AppliedBrokerFillIDs = append([]string(nil), o.AppliedBrokerFillIDs...)
	}
	return cloned
}

// accountHandle is broker.Account bound to one account of a Broker.
// Obtain one from Broker.OpenAccount.
type accountHandle struct {
	broker *Broker
	state  *accountState
}

var _ brokerpkg.Account = (*accountHandle)(nil)

// Reference implements broker.Account.
func (h *accountHandle) Reference() account.Reference {
	return h.state.ref
}

// Snapshot implements broker.Account.
func (h *accountHandle) Snapshot(ctx context.Context) (account.Snapshot, error) {
	if h.broker.isClosed() {
		return account.Snapshot{}, brokerpkg.ErrClosed
	}

	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	return h.state.snapshotLocked()
}

// Submit implements broker.Account. It validates req, accepts it into
// StatusWorking (no fill matching — see the package doc comment), and
// emits the resulting EventKindOrder event. Resubmitting the same
// req.OrderID is idempotent: Submit returns the already-stored Order
// unchanged and emits no additional event, matching Request.OrderID's
// role as the initial-submission idempotency key (ADR-017).
func (h *accountHandle) Submit(ctx context.Context, req order.Request) (order.Order, error) {
	if h.broker.isClosed() {
		return order.Order{}, brokerpkg.ErrClosed
	}

	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	if existing, ok := h.state.openOrders[req.OrderID]; ok {
		return cloneOrder(existing), nil
	}

	accepted := req.Quantity
	now := h.broker.deps.Clock.Now()

	// AcceptedLimitPrice/AcceptedStopPrice mirror the requested values
	// exactly: this package performs no price improvement or matching
	// (see the package doc comment), so whatever the request asked for
	// is what gets accepted.
	var acceptedLimit, acceptedStop *num.Price
	if req.LimitPrice != nil {
		v := *req.LimitPrice
		acceptedLimit = &v
	}
	if req.StopPrice != nil {
		v := *req.StopPrice
		acceptedStop = &v
	}

	o, err := order.NewOrder(order.Order{
		Request:            req,
		BrokerOrderID:      "sim-" + req.OrderID.String(),
		AcceptedQuantity:   &accepted,
		AcceptedLimitPrice: acceptedLimit,
		AcceptedStopPrice:  acceptedStop,
		Status:             order.StatusWorking,
		UpdatedAt:          now,
	})
	if err != nil {
		return order.Order{}, err
	}

	h.state.openOrders[req.OrderID] = cloneOrder(o)
	h.state.asOf = now
	if err := h.state.appendOrderEvent(h.broker.deps, o, req.Metadata.EventID); err != nil {
		return order.Order{}, err
	}
	return o, nil
}

// Cancel implements broker.Account. Cancel/replace lifecycle behavior
// is issue #151 (M3-08)'s scope, not this package's yet.
func (h *accountHandle) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	if h.broker.isClosed() {
		return order.CancelResult{}, brokerpkg.ErrClosed
	}
	return order.CancelResult{}, brokerpkg.ErrUnsupported
}

// Replace implements broker.Account. Cancel/replace lifecycle behavior
// is issue #151 (M3-08)'s scope, not this package's yet.
func (h *accountHandle) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	if h.broker.isClosed() {
		return order.ReplaceResult{}, brokerpkg.ErrClosed
	}
	return order.ReplaceResult{}, brokerpkg.ErrUnsupported
}

// Events implements broker.Account.
func (h *accountHandle) Events(ctx context.Context, cursor brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	if h.broker.isClosed() {
		return nil, brokerpkg.ErrClosed
	}
	return &eventReader{state: h.state, after: decodeCursor(cursor)}, nil
}
