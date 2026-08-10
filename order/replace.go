package order

import (
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
)

// ReplaceRequest asks a broker to modify an existing order's quantity
// and/or prices in place. As with CancelRequest, its Metadata.EventID
// identifies this specific replace command rather than serving as a
// reusable idempotency key across retries.
type ReplaceRequest struct {
	// OrderID identifies the order to replace.
	OrderID id.OrderID
	// NewQuantity, when non-nil, replaces the order's accepted
	// quantity.
	NewQuantity *num.Quantity
	// NewLimitPrice, when non-nil, replaces the order's accepted limit
	// price.
	NewLimitPrice *num.Price
	// NewStopPrice, when non-nil, replaces the order's accepted stop
	// price.
	NewStopPrice *num.Price
	Metadata     id.Metadata
}

// NewReplaceRequest validates and returns a ReplaceRequest. OrderID must
// be non-zero, and at least one of NewQuantity, NewLimitPrice, or
// NewStopPrice must be set — otherwise there is nothing to replace. A
// non-nil NewQuantity must be greater than zero, consistent with every
// other quantity in this package's vocabulary.
func NewReplaceRequest(r ReplaceRequest) (ReplaceRequest, error) {
	if r.OrderID.IsZero() {
		return ReplaceRequest{}, fmt.Errorf("%w: order id must be set", ErrInvalidReplaceRequest)
	}
	if r.NewQuantity == nil && r.NewLimitPrice == nil && r.NewStopPrice == nil {
		return ReplaceRequest{}, fmt.Errorf("%w: at least one of NewQuantity, NewLimitPrice, or NewStopPrice must be set", ErrInvalidReplaceRequest)
	}
	if r.NewQuantity != nil && r.NewQuantity.IsZero() {
		return ReplaceRequest{}, fmt.Errorf("%w: new quantity must be greater than zero", ErrInvalidReplaceRequest)
	}
	return r, nil
}

// ReplaceResult reports the outcome of a ReplaceRequest.
type ReplaceResult struct {
	OrderID id.OrderID
	// Status is the order's resulting status: typically StatusWorking
	// with the new values in effect, but a replace can also be
	// declined, in which case Rejection explains why.
	Status Status
	// Rejection explains why the replace could not be applied. Nil when
	// the replace succeeded.
	Rejection *Rejection
	Metadata  id.Metadata
}

// NewReplaceResult validates and returns a ReplaceResult. OrderID must
// be non-zero.
func NewReplaceResult(r ReplaceResult) (ReplaceResult, error) {
	if r.OrderID.IsZero() {
		return ReplaceResult{}, fmt.Errorf("%w: order id must be set", ErrInvalidReplaceResult)
	}
	return r, nil
}
