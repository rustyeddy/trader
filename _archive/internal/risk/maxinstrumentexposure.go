package risk

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// maxInstrumentExposureName is MaxInstrumentExposureRule's stable
// Rule.Name().
const maxInstrumentExposureName = "max_instrument_exposure"

// maxInstrumentExposureRule caps the notional value of the resulting
// position in one instrument after a proposal (issue #183, M4-08).
//
// Unlike maxPositionQuantityRule, this rule needs a price to value a
// quantity, and order.Proposal carries none for a Market order
// (execution.Planner's own v0 output, #179). It values the *entire*
// resulting quantity at Input.ReferencePrice — never a mix of
// ReferencePrice for the proposed delta and a Position's own
// historical AvgPrice for the already-held portion, which would blend
// two different valuation bases into one meaningless figure (review
// feedback on #183).
//
// It also exempts any de-risking proposal from the cap outright (issue
// #207), the same principle MaxPositionLeverageRule established
// (#184/PR #199) and ADR-034 records: an account already over this cap
// must always have a path back into compliance, never be trapped by
// the very rule a risk-reducing proposal would satisfy.
type maxInstrumentExposureRule struct {
	max num.Money
}

// NewMaxInstrumentExposureRule returns a Rule that rejects a proposal
// whose resulting position's notional value, valued at
// Input.ReferencePrice, would exceed max. max must be a valid,
// positive Money value.
func NewMaxInstrumentExposureRule(max num.Money) (Rule, error) {
	if !max.IsValid() {
		return nil, fmt.Errorf("%w: max exposure must have a valid currency", ErrInvalidRule)
	}
	zero, err := num.ParseMoney("0", max.Currency())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRule, err)
	}
	cmp, err := max.Cmp(zero)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRule, err)
	}
	if cmp <= 0 {
		return nil, fmt.Errorf("%w: max exposure must be positive", ErrInvalidRule)
	}
	return &maxInstrumentExposureRule{max: max}, nil
}

// Name implements Rule.
func (r *maxInstrumentExposureRule) Name() string { return maxInstrumentExposureName }

// Evaluate implements Rule.
func (r *maxInstrumentExposureRule) Evaluate(ctx context.Context, in Input) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, err
	}

	pos, hasPosition := findPosition(in.Account, in.Proposal.Listing)
	currentQty := num.Quantity{}
	if hasPosition {
		currentQty = pos.Quantity
	}

	resultSide, resultQty, err := resultingPosition(in.Account, in.Proposal)
	if err != nil {
		return RuleResult{}, fmt.Errorf("max instrument exposure: %w", err)
	}
	if resultSide == order.Flat || resultQty.IsZero() {
		return RuleResult{}, nil
	}

	// De-risking is exempt from the cap: see this rule's own doc
	// comment and ADR-034. Comparing magnitude directly (never the
	// current position's own AvgPrice) matches MaxPositionLeverageRule's
	// established comparison exactly.
	if resultQty.Cmp(currentQty) <= 0 {
		return RuleResult{}, nil
	}

	if in.ReferencePrice == nil || in.ReferencePrice.IsZero() {
		return RuleResult{}, fmt.Errorf("%w: max instrument exposure requires a positive Input.ReferencePrice for a proposal that results in an open position", ErrInsufficientRuleInput)
	}
	if !in.Proposal.Listing.Spec().SettlementCurrency().Equal(in.Account.Equity().Currency()) {
		return RuleResult{}, fmt.Errorf("%w: max instrument exposure: listing settlement currency %s does not match account equity currency %s",
			ErrInsufficientRuleInput, in.Proposal.Listing.Spec().SettlementCurrency(), in.Account.Equity().Currency())
	}
	if !r.max.Currency().Equal(in.Account.Equity().Currency()) {
		return RuleResult{}, fmt.Errorf("%w: max instrument exposure: configured limit currency %s does not match account equity currency %s",
			ErrInsufficientRuleInput, r.max.Currency(), in.Account.Equity().Currency())
	}

	valuePerUnit, err := in.ReferencePrice.MulRate(in.Proposal.Listing.Spec().Multiplier())
	if err != nil {
		return RuleResult{}, fmt.Errorf("max instrument exposure: computing value per unit: %w", err)
	}
	exposure, err := valuePerUnit.MulQuantity(resultQty, in.Account.Equity().Currency())
	if err != nil {
		return RuleResult{}, fmt.Errorf("max instrument exposure: computing exposure: %w", err)
	}

	cmp, err := exposure.Cmp(r.max)
	if err != nil {
		return RuleResult{}, fmt.Errorf("max instrument exposure: comparing exposure to limit: %w", err)
	}
	if cmp <= 0 {
		return RuleResult{}, nil
	}
	return RuleResult{
		Violations: []Violation{{
			Message:  fmt.Sprintf("resulting instrument exposure %s exceeds the maximum %s", exposure, r.max),
			Measured: exposure.String(),
			Limit:    r.max.String(),
		}},
	}, nil
}
