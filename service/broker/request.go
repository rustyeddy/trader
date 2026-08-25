package broker

import (
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
)

// ErrInvalidRequest marks a request that is missing a required field
// or names inconsistent account identities. It is never returned
// alongside a partial result.
var ErrInvalidRequest = errors.New("service/broker: invalid request")

// AccountRequest identifies which account an operation acts on. It is
// the common shape every account-scoped use case in this package
// embeds.
type AccountRequest struct {
	// AccountID identifies the account to act on. Required.
	AccountID id.AccountID
}

// Validate reports whether r is well-formed enough to attempt,
// returning a wrapped ErrInvalidRequest for the first problem found.
func (r AccountRequest) Validate() error {
	if r.AccountID.IsZero() {
		return fmt.Errorf("%w: account id is zero", ErrInvalidRequest)
	}
	return nil
}

// AccountsRequest is the request for the read-only Accounts use case:
// every account reference the wrapped Broker currently knows about. It
// carries no fields — account discovery is broker-wide, not
// account-scoped.
type AccountsRequest struct{}

// SnapshotRequest is the request for the read-only Snapshot use case:
// one account's current observed state.
type SnapshotRequest struct {
	AccountRequest
}

// SubmitRequest is the request for the mutating Submit use case: ask
// the account's broker to accept Order as a new order.
//
// Order.AccountID (via its embedded order.Proposal) must equal
// AccountRequest.AccountID — Validate reports ErrInvalidRequest if they
// disagree, rather than silently preferring one over the other. A
// transport adapter that reads AccountID from a URL path or CLI flag
// separately from the order body must ensure the two agree before
// calling Submit; Validate exists so a mismatch is never silently
// resolved by favoring whichever field happened to be checked first.
type SubmitRequest struct {
	AccountRequest
	Order order.Request
}

// Validate reports whether r is well-formed enough to attempt.
func (r SubmitRequest) Validate() error {
	if err := r.AccountRequest.Validate(); err != nil {
		return err
	}
	if r.Order.AccountID != r.AccountID {
		return fmt.Errorf("%w: order account id %s does not match request account id %s", ErrInvalidRequest, r.Order.AccountID, r.AccountID)
	}
	return nil
}

// CancelRequest is the request for the mutating Cancel use case: ask
// the account's broker to cancel an existing order. order.CancelRequest
// itself carries no account identity (see its own doc comment), so
// AccountRequest.AccountID is the only way this operation knows which
// account's order to cancel.
type CancelRequest struct {
	AccountRequest
	Cancel order.CancelRequest
}

// ReplaceRequest is the request for the mutating Replace use case: ask
// the account's broker to modify an existing order's quantity and/or
// prices in place. Like CancelRequest, order.ReplaceRequest carries no
// account identity of its own.
type ReplaceRequest struct {
	AccountRequest
	Replace order.ReplaceRequest
}
