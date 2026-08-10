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

// NewRequest validates and returns a Request for proposal and orderID.
// proposal is fully revalidated against the same rules NewProposal
// applies — not merely trusted by provenance — since Proposal's exported
// fields let a caller build one as a bare struct literal, bypassing
// NewProposal entirely. orderID must be non-zero.
func NewRequest(proposal Proposal, orderID id.OrderID) (Request, error) {
	r := Request{Proposal: proposal, OrderID: orderID}
	if err := checkRequest(r); err != nil {
		return Request{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return r, nil
}

// checkRequest validates r's fields, including its embedded Proposal,
// and returns a plain, unwrapped error, or nil. Shared with NewOrder for
// the same reason checkProposal is shared with NewRequest.
func checkRequest(r Request) error {
	if err := checkProposal(r.Proposal); err != nil {
		return fmt.Errorf("proposal: %w", err)
	}
	if r.OrderID.IsZero() {
		return fmt.Errorf("order id must be set")
	}
	return nil
}
