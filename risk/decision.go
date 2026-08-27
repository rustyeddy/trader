package risk

import (
	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Input is what Rule.Evaluate and Engine.Evaluate need to admit or
// reject one order.Proposal: the proposal itself and the account state
// to evaluate it against. Input stays minimal for v0 (issue #180,
// M4-05): no market snapshot, no multi-account portfolio.Portfolio —
// M4-01 explicitly keeps portfolio correlation out of this milestone,
// and every v0 risk policy this milestone lists can be evaluated from
// a proposal plus one account's own snapshot.
type Input struct {
	// Proposal is the candidate order under evaluation.
	Proposal order.Proposal
	// Account is the authoritative account state Proposal would trade
	// against.
	Account account.Snapshot

	// AdverseDistance is the adverse price-distance assumption this
	// proposal is being sized/admitted against — the same value
	// supplied to risk.Sizer at planning time (#181). It is an
	// assumption, not proof that a real broker stop order exists or
	// will ever be submitted: order.Proposal itself carries no
	// StopPrice for a Market order (execution.Planner's own v0 output,
	// #179), so a rule needing an adverse-price distance to compute
	// planned loss (for example PerTradeLossRule, #182) has nowhere
	// else to find one.
	//
	// AdverseDistance is optional at this Input level — Engine's own
	// checkInput does not require it, since most rules (#183, #184)
	// don't need one at all. A concrete rule that does need it, and
	// finds it nil or non-positive, returns a classifiable evaluation
	// error rather than treating a missing assumption as "no risk."
	AdverseDistance *num.Price
}

// Violation reports one Rule's finding that a Proposal is not
// acceptable. Rule names the rule that produced it (Rule.Name());
// Measured and Limit are each rule's own formatted representation of
// the values compared (for example "1500 USD" and "1000 USD") rather
// than a shared generic value type, since what is actually measured —
// money, quantity, a leverage ratio — varies per rule.
type Violation struct {
	Rule     string
	Message  string
	Measured string
	Limit    string
}

// Warning reports a non-blocking observation from a Rule: something
// worth surfacing without making the Proposal unacceptable.
type Warning struct {
	Rule    string
	Message string
}

// RuleResult is one Rule's own complete evaluation of an Input: empty
// Violations means the rule passed. A RuleResult may carry Warnings
// regardless of whether it also carries Violations.
type RuleResult struct {
	Rule       string
	Violations []Violation
	Warnings   []Warning
}

// Decision is risk admission's result for one Input (ADR-006,
// ADR-029): Allowed is true exactly when Violations is empty.
// Violations and Warnings are aggregated across every Rule an Engine
// evaluated, in the order those rules ran; RuleResults preserves each
// rule's own individual outcome for audit.
//
// Decision never carries an adjusted or alternate Proposal (ADR-029):
// risk evaluates the Proposal it was given and reports whether that
// exact proposal is acceptable, never a rewritten one. A rejected
// Proposal requires a new planning/sizing cycle to produce a revised
// one — an application/orchestration-layer decision, not something
// risk or execution decides on the caller's behalf.
type Decision struct {
	Allowed     bool
	Violations  []Violation
	Warnings    []Warning
	RuleResults []RuleResult
}
