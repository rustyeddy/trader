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

	// ErrInvalidSizeInput reports a SizeInput that fails validation:
	// an unconstructed Account/Listing, a non-positive RiskFraction or
	// StopDistance, or an account equity currency that does not match
	// the listing's settlement currency (ADR-030: Sizer performs no
	// implicit currency conversion).
	ErrInvalidSizeInput = errors.New("risk: invalid size input")

	// ErrSizeRoundsToZero reports that a Sizer's raw computed quantity
	// rounded down to zero at the listing's own quantity increment —
	// the account's risk budget, at this StopDistance, cannot buy even
	// one increment of this listing. This is a classifiable outcome,
	// never a silent zero-quantity Proposal.
	ErrSizeRoundsToZero = errors.New("risk: sizing rounds down to zero at this listing's quantity increment")

	// ErrInsufficientRuleInput reports that a Rule could not evaluate
	// Input because it required contextual data Input did not carry —
	// a missing/non-positive AdverseDistance, or a Proposal.Listing
	// settlement currency that does not match Account.Equity()'s
	// currency (no implicit conversion, matching ErrInvalidSizeInput's
	// own rule). This is distinct from ErrInvalidInput: Engine's own
	// checkInput deliberately does not require every field every Rule
	// might need (most rules need no AdverseDistance at all), so a
	// rule-specific requirement not being met is this rule's own
	// classification, not a structural Input defect Engine itself
	// should have caught.
	ErrInsufficientRuleInput = errors.New("risk: rule requires contextual input this Input does not carry")
)
