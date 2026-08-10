package order

import (
	"fmt"

	"github.com/rustyeddy/trader/id"
)

// Request is a Proposal that has been assigned a Trader order identity
// and is ready for broker submission. OrderID doubles as the
// idempotency key for that initial submission: a retried submission of
// the same Request reuses the same OrderID, and a broker adapter dedupes
// on it as the client order ID. This covers idempotency for creating the
// order — a later cancel or replace against it is a separate command
// with its own idempotency needs; see CancelRequest and ReplaceRequest.
type Request struct {
	Proposal
	// OrderID is Trader's own identifier for the order this request
	// would create.
	OrderID id.OrderID
}

// NewRequest validates and returns a Request for proposal, which must
// already be a validated Proposal (see NewProposal), and orderID, which
// must be non-zero.
func NewRequest(proposal Proposal, orderID id.OrderID) (Request, error) {
	if proposal.Listing.InstrumentID().IsZero() {
		return Request{}, fmt.Errorf("%w: proposal must be constructed", ErrInvalidRequest)
	}
	if orderID.IsZero() {
		return Request{}, fmt.Errorf("%w: order id must be set", ErrInvalidRequest)
	}
	return Request{Proposal: proposal, OrderID: orderID}, nil
}
