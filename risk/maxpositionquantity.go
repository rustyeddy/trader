package risk

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/num"
)

// maxPositionQuantityName is MaxPositionQuantityRule's stable
// Rule.Name().
const maxPositionQuantityName = "max_position_quantity"

// maxPositionQuantityRule caps the resulting position quantity in one
// instrument after a proposal (issue #183, M4-08): it evaluates
// resultingPosition, not the proposal's own gross quantity, so a
// reduce-only/closing proposal — whose resulting quantity is always
// smaller — passes without special-casing.
type maxPositionQuantityRule struct {
	max num.Quantity
}

// NewMaxPositionQuantityRule returns a Rule that rejects a proposal
// whose resulting position quantity would exceed max. max must be
// positive.
func NewMaxPositionQuantityRule(max num.Quantity) (Rule, error) {
	if max.IsZero() {
		return nil, fmt.Errorf("%w: max quantity must be positive", ErrInvalidRule)
	}
	return &maxPositionQuantityRule{max: max}, nil
}

// Name implements Rule.
func (r *maxPositionQuantityRule) Name() string { return maxPositionQuantityName }

// Evaluate implements Rule.
func (r *maxPositionQuantityRule) Evaluate(ctx context.Context, in Input) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, err
	}

	_, qty, err := resultingPosition(in.Account, in.Proposal)
	if err != nil {
		return RuleResult{}, fmt.Errorf("max position quantity: %w", err)
	}

	if qty.Cmp(r.max) <= 0 {
		return RuleResult{}, nil
	}
	return RuleResult{
		Violations: []Violation{{
			Message:  fmt.Sprintf("resulting position quantity %s exceeds the maximum %s", qty, r.max),
			Measured: qty.String(),
			Limit:    r.max.String(),
		}},
	}, nil
}
