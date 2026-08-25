package risk

import (
	"github.com/rustyeddy/trader/account"
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
