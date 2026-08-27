package risk

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// maxPositionLeverageName is MaxPositionLeverageRule's stable
// Rule.Name().
const maxPositionLeverageName = "max_position_leverage"

// maxPositionLeverageRule caps one resulting position's own leverage
// relative to account equity (issue #184, M4-09): the notional value
// of the position this proposal would result in, divided by a
// configured leverage ratio, must not exceed Account.Equity().
//
// This is deliberately named and scoped as *per-position* leverage,
// not account-wide leverage or true available-margin admission — see
// this rule's own package doc reference and the design discussion on
// #184. Every individual position passing this rule does not imply
// the account's aggregate leverage across all open positions stays
// within the same ratio; computing that faithfully would need a
// current mark for every open listing, the same capability #183
// explicitly deferred MaxAggregateExposureRule for. M3's simulator
// does not yet report leverage-aware BuyingPower/MarginUsed/
// MarginAvailable (verified directly against
// adapters/broker/sim/account.go: they mirror cash and zero), so this
// rule does not consult them at all — it independently re-derives
// required margin from ReferencePrice and the configured ratio,
// exactly like PerTradeLossRule (#182) and MaxInstrumentExposureRule
// (#183) each re-derive their own numbers rather than trusting
// anything upstream already computed.
type maxPositionLeverageRule struct {
	maxLeverage num.Rate
}

// NewMaxPositionLeverageRule returns a Rule that rejects a proposal
// whose resulting position's required margin — its notional value
// divided by maxLeverage — would exceed account equity. maxLeverage
// must be positive (for example 50 for a 50:1 ratio).
func NewMaxPositionLeverageRule(maxLeverage num.Rate) (Rule, error) {
	if maxLeverage.Sign() <= 0 {
		return nil, fmt.Errorf("%w: max leverage must be positive", ErrInvalidRule)
	}
	return &maxPositionLeverageRule{maxLeverage: maxLeverage}, nil
}

// Name implements Rule.
func (r *maxPositionLeverageRule) Name() string { return maxPositionLeverageName }

// Evaluate implements Rule.
func (r *maxPositionLeverageRule) Evaluate(ctx context.Context, in Input) (RuleResult, error) {
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
		return RuleResult{}, fmt.Errorf("max position leverage: %w", err)
	}
	if resultSide == order.Flat || resultQty.IsZero() {
		return RuleResult{}, nil
	}

	// De-risking is exempt from the cap: if the resulting position's
	// magnitude does not exceed the current position's own magnitude,
	// this proposal reduces (or leaves unchanged) absolute exposure —
	// and therefore required margin, since both are valued at the same
	// price/multiplier within one Evaluate call — regardless of
	// whether it also changed side. Comparing magnitude directly
	// (never touching the current position's own AvgPrice, per #183's
	// established valuation-basis rule) avoids trapping an account
	// that is already over this cap by rejecting the very moves that
	// would bring it back into compliance (review feedback on #199).
	// An add, a fresh position from Flat, or a reversal whose resulting
	// magnitude exceeds the current position's is still evaluated
	// against the cap normally below.
	if resultQty.Cmp(currentQty) <= 0 {
		return RuleResult{}, nil
	}

	if in.ReferencePrice == nil || in.ReferencePrice.IsZero() {
		return RuleResult{}, fmt.Errorf("%w: max position leverage requires a positive Input.ReferencePrice for a proposal that results in an open position", ErrInsufficientRuleInput)
	}
	if !in.Proposal.Listing.Spec().SettlementCurrency().Equal(in.Account.Equity().Currency()) {
		return RuleResult{}, fmt.Errorf("%w: max position leverage: listing settlement currency %s does not match account equity currency %s",
			ErrInsufficientRuleInput, in.Proposal.Listing.Spec().SettlementCurrency(), in.Account.Equity().Currency())
	}

	valuePerUnit, err := in.ReferencePrice.MulRate(in.Proposal.Listing.Spec().Multiplier())
	if err != nil {
		return RuleResult{}, fmt.Errorf("max position leverage: computing value per unit: %w", err)
	}
	notional, err := valuePerUnit.MulQuantity(resultQty, in.Account.Equity().Currency())
	if err != nil {
		return RuleResult{}, fmt.Errorf("max position leverage: computing notional: %w", err)
	}
	requiredMargin, err := notional.DivRate(r.maxLeverage)
	if err != nil {
		return RuleResult{}, fmt.Errorf("max position leverage: computing required margin: %w", err)
	}

	cmp, err := requiredMargin.Cmp(in.Account.Equity())
	if err != nil {
		return RuleResult{}, fmt.Errorf("max position leverage: comparing required margin to equity: %w", err)
	}
	if cmp <= 0 {
		return RuleResult{}, nil
	}
	return RuleResult{
		Violations: []Violation{{
			Message:  fmt.Sprintf("required margin %s at %s leverage exceeds account equity %s", requiredMargin, r.maxLeverage, in.Account.Equity()),
			Measured: requiredMargin.String(),
			Limit:    in.Account.Equity().String(),
		}},
	}, nil
}
