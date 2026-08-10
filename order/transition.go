package order

import (
	"fmt"
	"slices"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
)

// transitionGraph names every legal Status transition, keyed by the
// current Status. A Status absent as a key (StatusFilled, StatusCanceled,
// StatusRejected, StatusExpired) is terminal: nothing may follow it,
// except the always-legal idempotent same-status case CanTransitionTo
// handles separately.
//
// Two decisions this graph encodes deliberately:
//
//   - Reaching StatusCanceled, or a replace's resulting status, requires
//     having passed through StatusPendingCancel/StatusPendingReplace
//     first — there is no direct StatusWorking -> StatusCanceled edge.
//     A terminal cancel confirmation arriving without Trader ever having
//     locally recorded the pending cancel is treated as an out-of-order
//     event and rejected; reconciling that gap is a live-package
//     concern, not this package's.
//   - StatusRejected is reachable only from StatusPendingSubmit, and
//     StatusExpired only from StatusWorking/StatusPartiallyFilled,
//     matching real broker semantics (rejection happens pre-acceptance;
//     expiration happens to a still-live order) rather than leaving the
//     graph permissive by default.
var transitionGraph = map[Status][]Status{
	StatusPendingSubmit:   {StatusWorking, StatusRejected},
	StatusWorking:         {StatusPartiallyFilled, StatusFilled, StatusPendingCancel, StatusPendingReplace, StatusExpired},
	StatusPartiallyFilled: {StatusFilled, StatusPendingCancel, StatusPendingReplace, StatusExpired},
	StatusPendingCancel:   {StatusCanceled, StatusWorking, StatusPartiallyFilled, StatusFilled},
	StatusPendingReplace:  {StatusWorking, StatusPartiallyFilled, StatusFilled, StatusCanceled},
}

// CanTransitionTo reports whether an Order currently in Status from may
// legally move to Status to.
//
// to == from is always legal for any valid, known Status — including a
// terminal one — which is what makes redelivery of an unchanged status
// event (a broker resending "Filled" for an already-Filled order) a safe
// no-op rather than an error, matching the architecture document's
// at-least-once delivery assumption.
//
// StatusUnknown is never a legal transition target, including
// StatusUnknown -> StatusUnknown: M1-10 already made Unknown a safe
// value for an adapter to produce when it cannot classify a broker
// status, but this graph validates known, classifiable transitions —
// an Unknown target signals "this needs operator attention," not
// something to silently fold into the state machine.
func (from Status) CanTransitionTo(to Status) bool {
	if !from.valid() || !to.valid() || to == StatusUnknown {
		return false
	}
	if to == from {
		return true
	}
	return slices.Contains(transitionGraph[from], to)
}

// Terminal reports whether s is a terminal Status: no Apply* function
// moves an Order out of it, except the idempotent same-status case.
func (s Status) Terminal() bool {
	switch s {
	case StatusFilled, StatusCanceled, StatusRejected, StatusExpired:
		return true
	default:
		return false
	}
}

// ApplyAcceptance transitions o from StatusPendingSubmit to
// StatusWorking, recording the broker's own order identifier and its
// accepted quantity and prices. Unlike the result-applying functions
// below, ApplyAcceptance requires o to be exactly StatusPendingSubmit —
// not idempotently re-appliable from StatusWorking — since a redelivered
// acceptance carrying different accepted values would otherwise silently
// mutate an already-working order.
func ApplyAcceptance(o Order, brokerOrderID string, acceptedQuantity num.Quantity, acceptedLimitPrice, acceptedStopPrice *num.Price) (Order, error) {
	if o.Status != StatusPendingSubmit {
		return Order{}, fmt.Errorf("%w: cannot accept order in status %s", ErrIllegalTransition, o.Status)
	}
	o.BrokerOrderID = brokerOrderID
	o.AcceptedQuantity = &acceptedQuantity
	o.AcceptedLimitPrice = acceptedLimitPrice
	o.AcceptedStopPrice = acceptedStopPrice
	o.Status = StatusWorking
	return NewOrder(o)
}

// ApplyRejection transitions o from StatusPendingSubmit to
// StatusRejected, recording why the broker declined the order outright.
func ApplyRejection(o Order, rejection Rejection) (Order, error) {
	if o.Status != StatusPendingSubmit {
		return Order{}, fmt.Errorf("%w: cannot reject order in status %s", ErrIllegalTransition, o.Status)
	}
	o.Status = StatusRejected
	o.Rejection = &rejection
	return NewOrder(o)
}

// ApplyExpiration transitions o from StatusWorking or
// StatusPartiallyFilled to StatusExpired: its TimeInForce elapsed before
// it fully filled.
func ApplyExpiration(o Order) (Order, error) {
	if o.Status != StatusWorking && o.Status != StatusPartiallyFilled {
		return Order{}, fmt.Errorf("%w: cannot expire order in status %s", ErrIllegalTransition, o.Status)
	}
	o.Status = StatusExpired
	return NewOrder(o)
}

// ApplyCancelRequest transitions o from StatusWorking or
// StatusPartiallyFilled to StatusPendingCancel, marking a cancel as
// locally in flight before the broker confirms it. req.OrderID must
// match o, and req.Metadata.EventID must be non-zero: it becomes
// o.PendingCommandID, the correlation key ApplyCancelResult requires a
// result to match before applying it.
func ApplyCancelRequest(o Order, req CancelRequest) (Order, error) {
	if err := checkOrderIDMatch(o, req.OrderID); err != nil {
		return Order{}, err
	}
	if req.Metadata.EventID.IsZero() {
		return Order{}, fmt.Errorf("%w: cancel request must have a non-zero Metadata.EventID to correlate its result", ErrIllegalTransition)
	}
	if o.Status != StatusWorking && o.Status != StatusPartiallyFilled {
		return Order{}, fmt.Errorf("%w: cannot request cancel for order in status %s", ErrIllegalTransition, o.Status)
	}
	o.Status = StatusPendingCancel
	o.PendingCommandID = req.Metadata.EventID
	return NewOrder(o)
}

// ApplyCancelResult transitions o to result.Status: typically
// StatusCanceled, but a cancel can also be declined (result.Rejection
// explains why), in which case result.Status reflects the order's
// actual resulting state. result.OrderID must match o.
//
// If o.PendingCommandID is set (o is currently StatusPendingCancel with
// an outstanding command), result.Metadata.CausationID must match it —
// ErrStaleResult otherwise. This is what prevents a delayed result from
// an earlier cancel cycle on this same order from being misapplied
// during a later one. If o.PendingCommandID is unset — for example
// because ApplyFill already overrode a pending cancel with a complete
// fill — there is no outstanding command to correlate against, and
// result is validated purely against the transition graph: a result
// matching o's current (possibly terminal) Status is accepted as an
// idempotent no-op, satisfying redelivery of the same confirmation,
// while a result naming any other status is rejected as unreachable.
//
// ApplyCancelResult never copies result.Rejection into o.Rejection:
// those are distinct concepts. result.Rejection explains why the cancel
// itself was declined (for example, "already filled"); o.Rejection
// (see NewOrder) means the order itself was declined and is non-nil
// exactly when o.Status is StatusRejected — copying one into the other
// would violate that invariant the moment a cancel is declined against,
// say, a StatusFilled order.
func ApplyCancelResult(o Order, result CancelResult) (Order, error) {
	if err := checkOrderIDMatch(o, result.OrderID); err != nil {
		return Order{}, err
	}
	if !o.PendingCommandID.IsZero() && result.Metadata.CausationID != o.PendingCommandID {
		return Order{}, fmt.Errorf("%w: cancel result causation id %s does not match outstanding command %s", ErrStaleResult, result.Metadata.CausationID, o.PendingCommandID)
	}
	if !o.Status.CanTransitionTo(result.Status) {
		return Order{}, fmt.Errorf("%w: cancel result status %s not reachable from %s", ErrIllegalTransition, result.Status, o.Status)
	}
	o.Status = result.Status
	if o.Status != StatusPendingCancel {
		o.PendingCommandID = id.EventID{}
	}
	return NewOrder(o)
}

// ApplyReplaceRequest transitions o from StatusWorking or
// StatusPartiallyFilled to StatusPendingReplace, marking a replace as
// locally in flight before the broker confirms it. req.OrderID must
// match o, and req.Metadata.EventID must be non-zero: it becomes
// o.PendingCommandID, the correlation key ApplyReplaceResult requires a
// result to match before applying it. The requested new values are not
// applied yet — only ApplyReplaceResult applies them, once the broker
// has actually confirmed the replace.
func ApplyReplaceRequest(o Order, req ReplaceRequest) (Order, error) {
	if err := checkOrderIDMatch(o, req.OrderID); err != nil {
		return Order{}, err
	}
	if req.Metadata.EventID.IsZero() {
		return Order{}, fmt.Errorf("%w: replace request must have a non-zero Metadata.EventID to correlate its result", ErrIllegalTransition)
	}
	if o.Status != StatusWorking && o.Status != StatusPartiallyFilled {
		return Order{}, fmt.Errorf("%w: cannot request replace for order in status %s", ErrIllegalTransition, o.Status)
	}
	o.Status = StatusPendingReplace
	o.PendingCommandID = req.Metadata.EventID
	return NewOrder(o)
}

// ApplyReplaceResult transitions o to result.Status. request and result
// must both refer to o (their OrderIDs must match o.Request.OrderID and
// each other).
//
// If o.PendingCommandID is set, result.Metadata.CausationID must match
// it — ErrStaleResult otherwise — for the same reason as
// ApplyCancelResult: a delayed result from an earlier replace cycle on
// this same order must not be applied during a later one. If
// o.PendingCommandID is unset, result is validated purely against the
// transition graph, allowing a redelivered confirmation matching o's
// current status as an idempotent no-op.
//
// The replace is only treated as having taken effect — request's
// non-nil NewQuantity/NewLimitPrice/NewStopPrice replacing o's
// corresponding Accepted* fields — when result.Rejection is nil AND
// result.Status is StatusWorking or StatusPartiallyFilled: the order
// remaining live is what "the replacement took effect" actually means.
// A non-rejected result of StatusCanceled or StatusFilled means the
// order's life resolved a different way around the same time, and
// applying new accepted terms first would be misleading even though
// nothing was technically rejected. This model replace as in-place
// amendment of the same Trader OrderID, never as a distinct
// "superseded" order with its own identity; see ADR-018. On decline, or
// when the result does not mean success, o's accepted values are left
// unchanged.
func ApplyReplaceResult(o Order, request ReplaceRequest, result ReplaceResult) (Order, error) {
	if err := checkOrderIDMatch(o, request.OrderID); err != nil {
		return Order{}, err
	}
	if request.OrderID != result.OrderID {
		return Order{}, fmt.Errorf("%w: replace request order id %s does not match result order id %s", ErrOrderMismatch, request.OrderID, result.OrderID)
	}
	if !o.PendingCommandID.IsZero() && result.Metadata.CausationID != o.PendingCommandID {
		return Order{}, fmt.Errorf("%w: replace result causation id %s does not match outstanding command %s", ErrStaleResult, result.Metadata.CausationID, o.PendingCommandID)
	}
	if !o.Status.CanTransitionTo(result.Status) {
		return Order{}, fmt.Errorf("%w: replace result status %s not reachable from %s", ErrIllegalTransition, result.Status, o.Status)
	}

	amended := result.Rejection == nil && (result.Status == StatusWorking || result.Status == StatusPartiallyFilled)
	if amended {
		if request.NewQuantity != nil {
			o.AcceptedQuantity = request.NewQuantity
		}
		if request.NewLimitPrice != nil {
			o.AcceptedLimitPrice = request.NewLimitPrice
		}
		if request.NewStopPrice != nil {
			o.AcceptedStopPrice = request.NewStopPrice
		}
	}
	o.Status = result.Status
	if o.Status != StatusPendingReplace {
		o.PendingCommandID = id.EventID{}
	}
	return NewOrder(o)
}

// ApplyFill applies fill to o, accumulating FilledQuantity and
// recomputing Status. o must currently be StatusWorking,
// StatusPartiallyFilled, StatusPendingCancel, or StatusPendingReplace —
// live and accepted.
//
// fill's identifying fields must match o: OrderID, AccountID, Listing,
// and Side, and BrokerOrderID when both are non-empty. This rejects a
// perfectly valid Fill that simply belongs to a different order, which
// is a more serious failure mode than an illegal status transition.
//
// Duplicate delivery of the same broker execution is detected and
// safely ignored: fill.BrokerFillID is checked first, against
// o.AppliedBrokerFillIDs, since an adapter is not guaranteed to reuse
// the same Trader FillID across redelivery of one broker execution,
// while broker state is architecturally authoritative; fill.FillID
// against o.AppliedFillIDs is the fallback for adapters that do not
// report a BrokerFillID. On a detected duplicate, ApplyFill returns o
// unchanged alongside ErrDuplicateFill — detectable via errors.Is, but
// not a fatal error: the returned Order remains valid and current.
//
// A fill that would take FilledQuantity beyond AcceptedQuantity is
// rejected. A fill that exactly completes AcceptedQuantity moves o to
// StatusFilled regardless of its prior status, including
// StatusPendingCancel/StatusPendingReplace — there is nothing left to
// cancel or replace once the order is fully filled. A partial fill
// arriving while a cancel or replace is pending instead preserves
// StatusPendingCancel/StatusPendingReplace rather than downgrading to
// StatusPartiallyFilled, since the broker's pending cancel/replace
// confirmation may still arrive and should not be implicitly discarded.
//
// AvgFillPrice is cleared to nil, not recomputed: num has no
// Price-by-Quantity multiplication yet, so ApplyFill cannot correctly
// maintain a quantity-weighted average, and leaving a stale value from
// an earlier, smaller FilledQuantity would be a false snapshot.
func ApplyFill(o Order, fill Fill) (Order, error) {
	if err := checkFillIdentity(o, fill); err != nil {
		return Order{}, err
	}
	switch o.Status {
	case StatusWorking, StatusPartiallyFilled, StatusPendingCancel, StatusPendingReplace:
	default:
		return Order{}, fmt.Errorf("%w: cannot apply fill to order in status %s", ErrIllegalTransition, o.Status)
	}
	if o.hasAppliedFill(fill) {
		return o, ErrDuplicateFill
	}
	if o.AcceptedQuantity == nil {
		return Order{}, fmt.Errorf("%w: order has no accepted quantity to fill against", ErrIllegalTransition)
	}

	newFilled, err := o.FilledQuantity.Add(fill.Quantity)
	if err != nil {
		return Order{}, fmt.Errorf("%w: %v", ErrInvalidOrder, err)
	}
	if _, err := o.AcceptedQuantity.Sub(newFilled); err != nil {
		return Order{}, fmt.Errorf("%w: fill would exceed accepted quantity: %v", ErrInvalidOrder, err)
	}

	target := StatusPartiallyFilled
	switch {
	case newFilled.Equal(*o.AcceptedQuantity):
		target = StatusFilled
	case o.Status == StatusPendingCancel || o.Status == StatusPendingReplace:
		target = o.Status
	}
	// target is always one of StatusFilled, StatusPartiallyFilled, or
	// o.Status itself, and the switch above already restricts o.Status
	// to Working/PartiallyFilled/PendingCancel/PendingReplace — every
	// combination is already legal per transitionGraph, so there is no
	// CanTransitionTo check left to make here.

	o.FilledQuantity = newFilled
	o.AvgFillPrice = nil
	o.Status = target
	if o.Status != StatusPendingCancel && o.Status != StatusPendingReplace {
		o.PendingCommandID = id.EventID{}
	}
	o.AppliedFillIDs = append(append([]id.FillID(nil), o.AppliedFillIDs...), fill.FillID)
	if fill.BrokerFillID != "" {
		o.AppliedBrokerFillIDs = append(append([]string(nil), o.AppliedBrokerFillIDs...), fill.BrokerFillID)
	}
	return NewOrder(o)
}

// checkOrderIDMatch reports ErrOrderMismatch if eventOrderID does not
// identify the same order as o.
func checkOrderIDMatch(o Order, eventOrderID id.OrderID) error {
	if eventOrderID != o.Request.OrderID {
		return fmt.Errorf("%w: event order id %s does not match order %s", ErrOrderMismatch, eventOrderID, o.Request.OrderID)
	}
	return nil
}

func checkFillIdentity(o Order, fill Fill) error {
	if fill.OrderID != o.Request.OrderID {
		return fmt.Errorf("%w: fill order id %s does not match order %s", ErrOrderMismatch, fill.OrderID, o.Request.OrderID)
	}
	if fill.AccountID != o.Request.AccountID {
		return fmt.Errorf("%w: fill account id %s does not match order account id %s", ErrOrderMismatch, fill.AccountID, o.Request.AccountID)
	}
	if fill.Listing != o.Request.Listing {
		return fmt.Errorf("%w: fill listing does not match order listing", ErrOrderMismatch)
	}
	if fill.Side != o.Request.Side {
		return fmt.Errorf("%w: fill side %s does not match order side %s", ErrOrderMismatch, fill.Side, o.Request.Side)
	}
	if o.BrokerOrderID != "" && fill.BrokerOrderID != "" && fill.BrokerOrderID != o.BrokerOrderID {
		return fmt.Errorf("%w: fill broker order id %q does not match order broker order id %q", ErrOrderMismatch, fill.BrokerOrderID, o.BrokerOrderID)
	}
	return nil
}

func (o Order) hasAppliedFill(fill Fill) bool {
	if fill.BrokerFillID != "" && slices.Contains(o.AppliedBrokerFillIDs, fill.BrokerFillID) {
		return true
	}
	return slices.Contains(o.AppliedFillIDs, fill.FillID)
}
