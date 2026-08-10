package order

import "errors"

var (
	// ErrInvalidProposal reports a Proposal constructor argument that
	// fails validation.
	ErrInvalidProposal = errors.New("order: invalid proposal")

	// ErrInvalidRequest reports a Request constructor argument that
	// fails validation.
	ErrInvalidRequest = errors.New("order: invalid request")

	// ErrInvalidOrder reports an Order constructor argument, or a query
	// against an Order, that fails validation.
	ErrInvalidOrder = errors.New("order: invalid order")

	// ErrInvalidFill reports a Fill constructor argument that fails
	// validation.
	ErrInvalidFill = errors.New("order: invalid fill")

	// ErrInvalidCancelRequest reports a CancelRequest constructor
	// argument that fails validation.
	ErrInvalidCancelRequest = errors.New("order: invalid cancel request")

	// ErrInvalidCancelResult reports a CancelResult constructor argument
	// that fails validation.
	ErrInvalidCancelResult = errors.New("order: invalid cancel result")

	// ErrInvalidReplaceRequest reports a ReplaceRequest constructor
	// argument that fails validation.
	ErrInvalidReplaceRequest = errors.New("order: invalid replace request")

	// ErrInvalidReplaceResult reports a ReplaceResult constructor
	// argument that fails validation.
	ErrInvalidReplaceResult = errors.New("order: invalid replace result")

	// ErrInvalidPosition reports a Position constructor argument that
	// fails validation.
	ErrInvalidPosition = errors.New("order: invalid position")

	// ErrInvalidTrade reports a Trade constructor argument that fails
	// validation.
	ErrInvalidTrade = errors.New("order: invalid trade")

	// ErrIllegalTransition reports an Apply* lifecycle function call
	// whose requested Status is not reachable from the Order's current
	// Status — see order/transition.go's transition graph.
	ErrIllegalTransition = errors.New("order: illegal status transition")

	// ErrOrderMismatch reports an Apply* lifecycle function called with
	// an event (Fill, CancelResult, ReplaceRequest, ReplaceResult) whose
	// identifying fields (OrderID, AccountID, Listing, Side,
	// BrokerOrderID) do not match the Order it was applied to. This is
	// distinct from ErrIllegalTransition: the requested state change
	// might otherwise be legal, but the event does not belong to this
	// order at all.
	ErrOrderMismatch = errors.New("order: event does not match order identity")

	// ErrDuplicateFill reports that ApplyFill was called with a Fill
	// already recorded on the Order (by FillID or BrokerFillID). The
	// returned Order is unchanged and remains valid; callers may treat
	// this as a safe no-op rather than a fatal error.
	ErrDuplicateFill = errors.New("order: fill already applied")

	// ErrStaleResult reports that ApplyCancelResult or ApplyReplaceResult
	// was called with a result whose Metadata.CausationID does not match
	// Order.PendingCommandID: the result does not correlate to the
	// currently outstanding cancel/replace command, typically because it
	// is a delayed result from an earlier cancel/replace cycle on the
	// same order. This is distinct from ErrOrderMismatch, which means
	// the event belongs to a different order entirely.
	ErrStaleResult = errors.New("order: result does not correlate to the outstanding command")
)
