package account

import (
	"fmt"

	"github.com/rustyeddy/trader/id"
)

// Reference identifies one broker account without describing its state:
// just enough identity for account discovery (broker.Broker.Accounts)
// before opening a full operational handle. See Snapshot for observed
// state and the broker package's Account for the operational handle
// (ADR-007).
type Reference struct {
	// AccountID identifies the Trader-managed account.
	AccountID id.AccountID
	// Broker names the broker or provider this account belongs to, for
	// example "OANDA". It should case-insensitively match the Broker
	// field of any Snapshot describing the same account.
	Broker string
}

// NewReference validates and returns a Reference. AccountID must be
// non-zero and Broker must be non-empty.
func NewReference(r Reference) (Reference, error) {
	if r.AccountID.IsZero() {
		return Reference{}, fmt.Errorf("%w: account id must be set", ErrInvalidReference)
	}
	if r.Broker == "" {
		return Reference{}, fmt.Errorf("%w: broker must be set", ErrInvalidReference)
	}
	return r, nil
}
