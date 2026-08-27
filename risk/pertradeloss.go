package risk

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// perTradeLossName is PerTradeLossRule's stable Rule.Name().
const perTradeLossName = "per_trade_loss"

// perTradeLossRule is the v0 reference per-trade loss admission policy
// (issue #182, M4-07): it independently re-derives a proposal's
// planned loss and rejects it if that loss exceeds riskFraction of
// account equity — never trusting whatever risk.Sizer (#181) already
// computed, matching ADR-029's own approve/reject-only, no-mutation
// contract.
type perTradeLossRule struct {
	riskFraction num.Rate
}

// NewPerTradeLossRule returns a Rule that rejects a proposal whose
// planned loss exceeds riskFraction of account equity. riskFraction
// must be positive.
func NewPerTradeLossRule(riskFraction num.Rate) (Rule, error) {
	if riskFraction.Sign() <= 0 {
		return nil, fmt.Errorf("%w: risk fraction must be positive", ErrInvalidRule)
	}
	return &perTradeLossRule{riskFraction: riskFraction}, nil
}

// Name implements Rule.
func (r *perTradeLossRule) Name() string { return perTradeLossName }

// Evaluate implements Rule.
//
// Evaluate is exposure-aware, not a blind Proposal.Quantity x distance
// calculation: a ReduceOnly proposal (execution's own v0 Exit/partial-
// reduce output, #179) never opens new exposure, so it passes without
// requiring AdverseDistance at all. A non-ReduceOnly proposal that
// reverses an existing position (crosses through flat) is charged only
// for the portion of its quantity beyond the position being closed —
// the actual new exposure it would create — not its full broker order
// quantity. Aggregate/existing-position exposure limits are #183's own
// responsibility, not this rule's.
func (r *perTradeLossRule) Evaluate(ctx context.Context, in Input) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, err
	}

	riskQty, err := newRiskQuantity(in.Account, in.Proposal)
	if err != nil {
		return RuleResult{}, fmt.Errorf("per-trade loss: %w", err)
	}
	if riskQty.IsZero() {
		return RuleResult{}, nil
	}

	if in.AdverseDistance == nil || in.AdverseDistance.IsZero() {
		return RuleResult{}, fmt.Errorf("%w: per-trade loss requires a positive Input.AdverseDistance for a proposal that opens new exposure", ErrInsufficientRuleInput)
	}
	if !in.Proposal.Listing.Spec().SettlementCurrency().Equal(in.Account.Equity().Currency()) {
		return RuleResult{}, fmt.Errorf("%w: per-trade loss: listing settlement currency %s does not match account equity currency %s",
			ErrInsufficientRuleInput, in.Proposal.Listing.Spec().SettlementCurrency(), in.Account.Equity().Currency())
	}
	distance := *in.AdverseDistance

	lossPerUnit, err := distance.MulRate(in.Proposal.Listing.Spec().Multiplier())
	if err != nil {
		return RuleResult{}, fmt.Errorf("per-trade loss: computing loss per unit: %w", err)
	}
	plannedLoss, err := lossPerUnit.MulQuantity(riskQty, in.Account.Equity().Currency())
	if err != nil {
		return RuleResult{}, fmt.Errorf("per-trade loss: computing planned loss: %w", err)
	}
	budget, err := in.Account.Equity().MulRate(r.riskFraction)
	if err != nil {
		return RuleResult{}, fmt.Errorf("per-trade loss: computing risk budget: %w", err)
	}

	cmp, err := plannedLoss.Cmp(budget)
	if err != nil {
		return RuleResult{}, fmt.Errorf("per-trade loss: comparing planned loss to budget: %w", err)
	}
	if cmp <= 0 {
		return RuleResult{}, nil
	}

	return RuleResult{
		Violations: []Violation{{
			Message:  fmt.Sprintf("planned loss %s exceeds the per-trade risk budget %s", plannedLoss, budget),
			Measured: plannedLoss.String(),
			Limit:    budget.String(),
		}},
	}, nil
}

// newRiskQuantity returns the portion of proposal.Quantity that would
// open genuinely new exposure against acc's current position in
// proposal.Listing's instrument — zero for any proposal that only
// reduces or exactly closes existing exposure.
//
//   - ReduceOnly: always zero, by construction (execution never marks
//     an exposure-opening proposal ReduceOnly).
//   - No current position, or a Flat one: the full proposal quantity
//     opens fresh exposure.
//   - Same direction as the current position (adding): the full
//     proposal quantity is incremental new exposure.
//   - Opposite direction, proposal quantity <= current position
//     quantity: a pure reduction (or exact close) — zero.
//   - Opposite direction, proposal quantity > current position
//     quantity: a reversal through flat — only the excess beyond the
//     current position's own quantity opens new exposure.
func newRiskQuantity(acc account.Snapshot, proposal order.Proposal) (num.Quantity, error) {
	if proposal.ReduceOnly {
		return num.Quantity{}, nil
	}

	pos, ok := findPosition(acc, proposal.Listing)
	if !ok || pos.Side == order.Flat {
		return proposal.Quantity, nil
	}

	sameDirection := (pos.Side == order.Long && proposal.Side == order.Buy) ||
		(pos.Side == order.Short && proposal.Side == order.Sell)
	if sameDirection {
		return proposal.Quantity, nil
	}

	if proposal.Quantity.Cmp(pos.Quantity) <= 0 {
		return num.Quantity{}, nil
	}
	return proposal.Quantity.Sub(pos.Quantity)
}

// findPosition returns acc's open position in listing's instrument, if
// any. Position matching is by instrument identity, not exact Listing
// equality — the same convention execution.findPosition uses (#179) —
// but reimplemented locally here rather than imported: risk must never
// import execution (ADR-006, package-boundaries.org).
func findPosition(acc account.Snapshot, listing instrument.Listing) (order.Position, bool) {
	for _, p := range acc.Positions() {
		if p.Listing.InstrumentID().Equal(listing.InstrumentID()) {
			return p, true
		}
	}
	return order.Position{}, false
}
