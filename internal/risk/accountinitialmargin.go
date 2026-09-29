package risk

import (
	"context"
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/account/margin"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// accountInitialMarginName is AccountInitialMarginRule's stable
// Rule.Name().
const accountInitialMarginName = "account_initial_margin"

// accountInitialMarginRule is the account-level initial-margin
// admission rule (ADR-066, issue #413): a proposal that increases
// exposure is rejected when the account's prospective gross notional ×
// initial_margin_ratio would exceed equity.
//
// Unlike MaxPositionLeverageRule, which bounds one position at a time
// (ADR-034), this rule bounds the whole account: every open position
// counts, longs and shorts both add, and nothing is netted. The
// arithmetic is the shared internal/account/margin calculation, which
// the simulator's fill-time check (#415) will also use, so admission
// and fill agree on what gross exposure is.
//
// # Listing-level positions
//
// Positions are matched by account.ListingKey — the unit at which an
// account nets one position — not by instrument alone. The existing
// per-instrument rules (MaxPositionQuantityRule,
// MaxInstrumentExposureRule) keep findPosition's instrument-level
// aggregation, which is what "per instrument" means for them; margin,
// by contrast, must follow what the broker actually holds, so a
// proposal in one listing never adds to a position in another listing
// of the same instrument.
//
// # De-risking needs no prices
//
// Whether a proposal is de-risking is decided from quantities alone,
// before any price is consulted: if the resulting position in the
// proposal's listing is no larger than the current one — a full
// close, a partial reduction, or a reversal that doesn't grow — the
// proposal is admitted, even on an account already over the limit and
// even with no ReferencePrice or marks (ADR-066, ADR-034). Prices are
// required only to evaluate a proposal that increases exposure.
//
// # Valuation
//
// For an increasing proposal, the proposal's own listing is valued at
// Input.ReferencePrice in both the current and prospective states;
// every other open position at its snapshot mark (account.Snapshot
// .Marks, #412), never its AvgPrice. Equity is the snapshot's pre-fee
// Equity(); this rule cannot see the commission model. Exact equality
// with equity is admitted.
type accountInitialMarginRule struct {
	policy margin.Ratio
}

// NewAccountInitialMarginRule returns a Rule that rejects a proposal
// whose prospective account-wide required initial margin — gross
// notional × ratio — would exceed equity. ratio must be positive: 1.0
// is unlevered, 0.5 permits 2× gross exposure, 0.25 permits 4×.
func NewAccountInitialMarginRule(ratio num.Rate) (Rule, error) {
	policy, err := margin.NewRatio(ratio)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRule, err)
	}
	return &accountInitialMarginRule{policy: policy}, nil
}

// Name implements Rule.
func (r *accountInitialMarginRule) Name() string { return accountInitialMarginName }

// Evaluate implements Rule.
func (r *accountInitialMarginRule) Evaluate(ctx context.Context, in Input) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, err
	}

	key := account.KeyOf(in.Proposal.Listing)
	curSide, curQty := order.Flat, num.Quantity{}
	if pos, ok := findListingPosition(in.Account, key); ok {
		curSide, curQty = pos.Side, pos.Quantity
	}
	_, resultQty, err := resultingFrom(curSide, curQty, in.Proposal)
	if err != nil {
		return RuleResult{}, fmt.Errorf("account initial margin: %w", err)
	}
	if resultQty.Cmp(curQty) <= 0 {
		return RuleResult{}, nil // de-risking or unchanged: admitted from quantities alone
	}

	if in.ReferencePrice == nil || in.ReferencePrice.IsZero() {
		return RuleResult{}, fmt.Errorf("%w: account initial margin requires a positive Input.ReferencePrice for a proposal that increases exposure", ErrInsufficientRuleInput)
	}

	marks := make(margin.Marks)
	for _, m := range in.Account.Marks() {
		marks[m.Listing] = m.Price
	}
	a, err := margin.Assess(in.Account.Positions(), marks, margin.Change{
		Listing:   in.Proposal.Listing,
		Resulting: resultQty,
		Price:     *in.ReferencePrice,
	}, r.policy, in.Account.Currency())
	if err != nil {
		if errors.Is(err, margin.ErrMissingMark) || errors.Is(err, margin.ErrCurrencyMismatch) {
			return RuleResult{}, fmt.Errorf("%w: account initial margin: %v", ErrInsufficientRuleInput, err)
		}
		return RuleResult{}, fmt.Errorf("account initial margin: %w", err)
	}

	equity := in.Account.Equity()
	ok, err := a.Prospective.Within(equity)
	if err != nil {
		return RuleResult{}, fmt.Errorf("account initial margin: %w", err)
	}
	if ok {
		return RuleResult{}, nil
	}
	return RuleResult{
		Violations: []Violation{{
			Message: fmt.Sprintf("prospective required margin %s (gross notional %s, up from %s, at initial margin ratio %s) exceeds account equity %s",
				a.Prospective.Required, a.Prospective.Gross, a.Current.Gross, r.policy.Value(), equity),
			Measured: a.Prospective.Required.String(),
			Limit:    equity.String(),
		}},
	}, nil
}
