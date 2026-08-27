package risk

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/order"
)

// maxOpenPositionsName is MaxOpenPositionsRule's stable Rule.Name().
const maxOpenPositionsName = "max_open_positions"

// maxOpenPositionsRule caps the number of distinct open positions an
// account may hold (issue #183, M4-08).
//
// It derives whether the count would actually change from
// resultingPosition's before/after state, not from Proposal.ReduceOnly
// alone (review feedback on #183): a non-ReduceOnly proposal that
// exactly closes a position still reduces the count; a reversal
// crosses Flat but ends with one position in the same instrument, so
// the count is unchanged; a proposal on an instrument the account
// doesn't already hold increases the count only if its resulting
// quantity is non-zero. The rule only ever blocks a count *increase*
// — reducing or leaving the count unchanged always passes.
type maxOpenPositionsRule struct {
	max int
}

// NewMaxOpenPositionsRule returns a Rule that rejects a proposal that
// would grow the account's number of distinct open positions past
// max. max must be positive.
func NewMaxOpenPositionsRule(max int) (Rule, error) {
	if max <= 0 {
		return nil, fmt.Errorf("%w: max open positions must be positive", ErrInvalidRule)
	}
	return &maxOpenPositionsRule{max: max}, nil
}

// Name implements Rule.
func (r *maxOpenPositionsRule) Name() string { return maxOpenPositionsName }

// Evaluate implements Rule.
func (r *maxOpenPositionsRule) Evaluate(ctx context.Context, in Input) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, err
	}

	_, wasOpen := findPosition(in.Account, in.Proposal.Listing)

	resultSide, resultQty, err := resultingPosition(in.Account, in.Proposal)
	if err != nil {
		return RuleResult{}, fmt.Errorf("max open positions: %w", err)
	}
	willBeOpen := resultSide != order.Flat && !resultQty.IsZero()

	delta := 0
	switch {
	case !wasOpen && willBeOpen:
		delta = 1
	case wasOpen && !willBeOpen:
		delta = -1
	}
	if delta <= 0 {
		return RuleResult{}, nil
	}

	newCount := len(in.Account.Positions()) + delta
	if newCount <= r.max {
		return RuleResult{}, nil
	}
	return RuleResult{
		Violations: []Violation{{
			Message:  fmt.Sprintf("resulting open position count %d exceeds the maximum %d", newCount, r.max),
			Measured: fmt.Sprintf("%d", newCount),
			Limit:    fmt.Sprintf("%d", r.max),
		}},
	}, nil
}
