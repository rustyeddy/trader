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

	// ErrInvalidReplaceRequest reports a ReplaceRequest constructor
	// argument that fails validation.
	ErrInvalidReplaceRequest = errors.New("order: invalid replace request")

	// ErrInvalidPosition reports a Position constructor argument that
	// fails validation.
	ErrInvalidPosition = errors.New("order: invalid position")

	// ErrInvalidTrade reports a Trade constructor argument that fails
	// validation.
	ErrInvalidTrade = errors.New("order: invalid trade")
)
