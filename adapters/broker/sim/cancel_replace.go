package sim

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"

	brokerpkg "github.com/rustyeddy/trader/broker"
)

// Cancel implements broker.Account (issue #151, M3-08). This
// simulator has no real broker latency, so a cancel resolves
// synchronously within one call: o transitions StatusWorking/
// StatusPartiallyFilled -> StatusPendingCancel -> StatusCanceled,
// emitting one EventKindOrder event per transition (causally chained),
// and Cancel returns the resulting CancelResult directly rather than
// leaving StatusPendingCancel as durable, later-observable state —
// there is no separate confirmation to arrive afterward the way there
// would be against a real broker.
//
// Three outcomes:
//
//   - o is StatusWorking/StatusPartiallyFilled: cancels normally (the
//     two-event sequence above).
//   - o is already StatusCanceled: idempotent no-op — returns the same
//     CancelResult, emits no additional event. Unlike Submit's
//     OrderID-based idempotency (ADR-017), this is not a keyed dedup:
//     CancelRequest.Metadata.EventID is not a reusable idempotency key
//     (see CancelRequest's doc comment), so a second Cancel of an
//     already-canceled order is simply re-evaluated and happens to
//     already be in its target state.
//   - o is any other terminal status (Filled, Rejected, Expired): the
//     cancel is declined. CancelResult.Status reports o's actual
//     status and Rejection explains why; nothing about o changes, so
//     no event is emitted — there is no transition to report.
func (h *accountHandle) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	if h.broker.isClosed() {
		return order.CancelResult{}, brokerpkg.ErrClosed
	}
	req, err := order.NewCancelRequest(req)
	if err != nil {
		return order.CancelResult{}, err
	}

	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	if h.state.closed {
		return order.CancelResult{}, brokerpkg.ErrClosed
	}

	o, ok := h.state.orders[req.OrderID]
	if !ok {
		return order.CancelResult{}, brokerpkg.ErrOrderNotFound
	}

	if o.Status == order.StatusCanceled {
		return order.NewCancelResult(order.CancelResult{OrderID: req.OrderID, Status: order.StatusCanceled})
	}

	if o.Status != order.StatusWorking && o.Status != order.StatusPartiallyFilled {
		return order.NewCancelResult(order.CancelResult{
			OrderID: req.OrderID,
			Status:  o.Status,
			Rejection: &order.Rejection{
				Reason: order.ReasonUnknown,
				Detail: fmt.Sprintf("order is in terminal status %s and cannot be canceled", o.Status),
			},
		})
	}

	// Build every event and result value before mutating any state,
	// matching the atomicity discipline Submit/Advance already
	// establish: a failure anywhere below must leave h.state exactly
	// as it was.
	pending, err := order.ApplyCancelRequest(o, req)
	if err != nil {
		return order.CancelResult{}, err
	}
	pendingEvent, err := h.state.buildOrderEvent(h.broker.deps, pending, req.Metadata.EventID, h.state.nextSequence+1)
	if err != nil {
		return order.CancelResult{}, err
	}

	result, err := order.NewCancelResult(order.CancelResult{
		OrderID:  req.OrderID,
		Status:   order.StatusCanceled,
		Metadata: id.Metadata{CausationID: req.Metadata.EventID, Timestamp: h.broker.deps.Clock.Now()},
	})
	if err != nil {
		return order.CancelResult{}, err
	}

	canceled, err := order.ApplyCancelResult(pending, result)
	if err != nil {
		return order.CancelResult{}, err
	}
	canceledEvent, err := h.state.buildOrderEvent(h.broker.deps, canceled, pendingEvent.Metadata.EventID, h.state.nextSequence+2)
	if err != nil {
		return order.CancelResult{}, err
	}

	h.state.orders[req.OrderID] = cloneOrder(canceled)
	h.state.asOf = h.broker.deps.Clock.Now()
	h.state.commitEvents(pendingEvent, canceledEvent)
	return result, nil
}

// Replace implements broker.Account (issue #151, M3-08). Like Cancel,
// a replace resolves synchronously within one call: o transitions
// StatusWorking/StatusPartiallyFilled -> StatusPendingReplace -> its
// resulting status, emitting one EventKindOrder event per transition.
//
// Whether the replace is accepted or declined is decided by
// revalidating o with req's new values applied through order.NewOrder
// — the same tick size, quantity increment, and filled-quantity rules
// every other order construction obeys. If validation passes, the new
// values take effect and o returns to its pre-replace status (Working
// or PartiallyFilled). If validation fails (for example a new quantity
// below what already filled, or a price that violates the listing's
// tick size), the replace is declined: o's accepted values are left
// unchanged and ReplaceResult.Rejection explains why, per ADR-018 —
// this never sets Order.Rejection, which is reserved for an order
// declined outright at submission.
//
// An order that is not currently StatusWorking/StatusPartiallyFilled
// is declined without any transition or event, the same as Cancel's
// equivalent case. Unlike Cancel, there is no separate "already
// replaced" idempotent case: ReplaceRequest.Metadata.EventID is not a
// reusable idempotency key either (see ReplaceRequest's doc comment),
// so a repeated Replace against an already-amended, still-live order
// is simply evaluated fresh against its current state.
func (h *accountHandle) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	if h.broker.isClosed() {
		return order.ReplaceResult{}, brokerpkg.ErrClosed
	}
	req, err := order.NewReplaceRequest(req)
	if err != nil {
		return order.ReplaceResult{}, err
	}

	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	if h.state.closed {
		return order.ReplaceResult{}, brokerpkg.ErrClosed
	}

	o, ok := h.state.orders[req.OrderID]
	if !ok {
		return order.ReplaceResult{}, brokerpkg.ErrOrderNotFound
	}

	if o.Status != order.StatusWorking && o.Status != order.StatusPartiallyFilled {
		return order.NewReplaceResult(order.ReplaceResult{
			OrderID: req.OrderID,
			Status:  o.Status,
			Rejection: &order.Rejection{
				Reason: order.ReasonUnknown,
				Detail: fmt.Sprintf("order is in terminal status %s and cannot be replaced", o.Status),
			},
		})
	}

	pending, err := order.ApplyReplaceRequest(o, req)
	if err != nil {
		return order.ReplaceResult{}, err
	}
	pendingEvent, err := h.state.buildOrderEvent(h.broker.deps, pending, req.Metadata.EventID, h.state.nextSequence+1)
	if err != nil {
		return order.ReplaceResult{}, err
	}

	proposed := pending
	if req.NewQuantity != nil {
		proposed.AcceptedQuantity = req.NewQuantity
	}
	if req.NewLimitPrice != nil {
		proposed.AcceptedLimitPrice = req.NewLimitPrice
	}
	if req.NewStopPrice != nil {
		proposed.AcceptedStopPrice = req.NewStopPrice
	}
	_, validationErr := order.NewOrder(proposed)

	resultMeta := id.Metadata{CausationID: req.Metadata.EventID, Timestamp: h.broker.deps.Clock.Now()}
	var result order.ReplaceResult
	if validationErr != nil {
		result, err = order.NewReplaceResult(order.ReplaceResult{
			OrderID: req.OrderID,
			Status:  o.Status,
			Rejection: &order.Rejection{
				Reason: order.ReasonUnknown,
				Detail: validationErr.Error(),
			},
			Metadata: resultMeta,
		})
	} else {
		result, err = order.NewReplaceResult(order.ReplaceResult{
			OrderID:  req.OrderID,
			Status:   o.Status,
			Metadata: resultMeta,
		})
	}
	if err != nil {
		return order.ReplaceResult{}, err
	}

	replaced, err := order.ApplyReplaceResult(pending, req, result)
	if err != nil {
		return order.ReplaceResult{}, err
	}
	replacedEvent, err := h.state.buildOrderEvent(h.broker.deps, replaced, pendingEvent.Metadata.EventID, h.state.nextSequence+2)
	if err != nil {
		return order.ReplaceResult{}, err
	}

	h.state.orders[req.OrderID] = cloneOrder(replaced)
	h.state.asOf = h.broker.deps.Clock.Now()
	h.state.commitEvents(pendingEvent, replacedEvent)
	return result, nil
}
