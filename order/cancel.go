package order

import (
	"fmt"

	"github.com/rustyeddy/trader/id"
)

// CancelRequest asks a broker to cancel an existing order. Its
// Metadata.EventID identifies this specific cancel command; it is not a
// reusable idempotency key the way Request.OrderID is for initial
// submission — a retried CancelRequest is a separate command concern,
// deferred until a real out-of-process or unreliable-transport
// requirement makes it necessary.
type CancelRequest struct {
	// OrderID identifies the order to cancel.
	OrderID  id.OrderID
	Metadata id.Metadata
}

// NewCancelRequest validates and returns a CancelRequest. OrderID must
// be non-zero.
func NewCancelRequest(r CancelRequest) (CancelRequest, error) {
	if r.OrderID.IsZero() {
		return CancelRequest{}, fmt.Errorf("%w: order id must be set", ErrInvalidCancelRequest)
	}
	return r, nil
}

// CancelResult reports the outcome of a CancelRequest.
type CancelResult struct {
	OrderID id.OrderID
	// Status is the order's resulting status: typically StatusCanceled,
	// but a cancel can also be declined — for example if the order
	// already filled — in which case Status reflects the order's actual
	// state and Rejection explains why the cancel itself did not apply.
	Status Status
	// Rejection explains why the cancel could not be applied. Nil when
	// the cancel succeeded.
	Rejection *Rejection
	Metadata  id.Metadata
}

// NewCancelResult validates and returns a CancelResult. OrderID must be
// non-zero.
func NewCancelResult(r CancelResult) (CancelResult, error) {
	if r.OrderID.IsZero() {
		return CancelResult{}, fmt.Errorf("%w: order id must be set", ErrInvalidCancelResult)
	}
	return r, nil
}
