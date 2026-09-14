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
	// an unconstructed Account/Listing, an account equity currency
	// that does not match the listing's settlement currency (ADR-030:
	// Sizer performs no implicit currency conversion), or — checked by
	// each concrete Sizer itself, not by the shared checkSizeInput
	// (ADR-061) — a non-positive RiskFraction/StopDistance/
	// ReferencePrice that Sizer implementation actually requires.
	ErrInvalidSizeInput = errors.New("risk: invalid size input")

	// ErrSizeRoundsToZero reports that a Sizer's raw computed quantity
	// rounded down to zero at the listing's own quantity increment —
	// the account's own sizing budget cannot buy even one increment of
	// this listing at the price/distance the configured Sizer computed
	// it from (fixedFractionSizer's risk budget at StopDistance, or
	// fullNotionalSizer's available capital at ReferencePrice, ADR-061
	// — the specific budget/price pairing depends on which Sizer
	// raised it). This is a classifiable outcome, never a silent
	// zero-quantity Proposal.
	ErrSizeRoundsToZero = errors.New("risk: sizing rounds down to zero at this listing's quantity increment")

	// ErrFullNotionalRequiresFlat reports that fullNotionalSizer
	// (ADR-061) was asked to size a position for an instrument the
	// account already holds one in. order.IntentEnter means "open or
	// increase," but fullNotionalSizer always computes a brand-new
	// order equal to its entire available budget; sizing a second such
	// order on top of an existing position could push total exposure
	// past the 100%-of-equity ceiling this Sizer's whole contract
	// promises never to exceed. See fullNotionalSizer's own doc
	// comment for why this is a flat-only precondition rather than a
	// target-exposure/delta computation.
	ErrFullNotionalRequiresFlat = errors.New("risk: full-notional sizing requires no existing position in this instrument")

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
