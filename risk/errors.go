package risk

import "errors"

var (
	// ErrInvalidInput reports an Input that fails validation: an
	// invalid Proposal, an unconstructed Account, a Proposal.AccountID
	// that does not match Account.AccountID(), or a Proposal.Listing
	// whose Provider does not match Account.Broker(). Malformed input
	// is rejected before any Rule runs — it is not a policy violation,
	// and every Rule should be able to assume Input is already
	// structurally sound.
	ErrInvalidInput = errors.New("risk: invalid input")

	// ErrInvalidRule reports a Rule passed to NewEngine that is nil or
	// has an empty Name().
	ErrInvalidRule = errors.New("risk: invalid rule")
)
