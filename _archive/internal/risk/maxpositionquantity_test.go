package risk

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMaxPositionQuantityRuleRejectsZero(t *testing.T) {
	_, err := NewMaxPositionQuantityRule(num.Quantity{})
	require.ErrorIs(t, err, ErrInvalidRule)
}

func TestMaxPositionQuantityRuleName(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("1000"))
	require.NoError(t, err)
	assert.Equal(t, "max_position_quantity", r.Name())
}

func TestMaxPositionQuantityRuleWithinLimitPasses(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("1000"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

func TestMaxPositionQuantityRuleExceedsLimitViolates(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("1000"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1001", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "1001", result.Violations[0].Measured)
	assert.Equal(t, "1000", result.Violations[0].Limit)
}

func TestMaxPositionQuantityRuleAddingPastLimitViolates(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("1000"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "800")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "300", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "1100", result.Violations[0].Measured)
}

// TestMaxPositionQuantityRuleReduceOnlyNeverBlocked proves a
// ReduceOnly proposal that would already breach the limit if it were
// treated as gross quantity still passes, since the resulting quantity
// only ever shrinks.
func TestMaxPositionQuantityRuleReduceOnlyNeverBlocked(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("100"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "5000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "5000", true)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestMaxPositionQuantityRuleReduceOnlyDeRiskingAlwaysPasses is issue
// #207's own regression, mirroring MaxPositionLeverageRule's identical
// fix (#184/PR #199, ADR-034): an account already over the configured
// cap must not be trapped there by having a genuinely risk-reducing
// move rejected merely because the *resulting* position is still over
// the cap. Existing 5000 already exceeds the 100 cap; reducing by 50
// to 4950 is still over the cap in isolation, but the reduction itself
// must be allowed, since it strictly decreases exposure.
func TestMaxPositionQuantityRuleReduceOnlyDeRiskingAlwaysPasses(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("100"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "5000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "50", true)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations, "a de-risking move must never be blocked, even if the resulting position remains over the cap")
}

// TestMaxPositionQuantityRuleNonReduceOnlyPartialDeRiskingAlwaysPasses
// is the same invariant as the ReduceOnly case above, for a
// non-ReduceOnly proposal that is still a strict partial reduction.
func TestMaxPositionQuantityRuleNonReduceOnlyPartialDeRiskingAlwaysPasses(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("100"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "5000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "50", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestMaxPositionQuantityRuleReversalPastCurrentMagnitudeIsEvaluatedNormally
// covers the boundary of the de-risking exemption itself: starting
// from an already over-limit position, a reversal whose *resulting*
// magnitude exceeds the current position's is not de-risking — it is
// evaluated against the cap exactly as any other increase would be.
func TestMaxPositionQuantityRuleReversalPastCurrentMagnitudeIsEvaluatedNormally(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("100"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "5000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "11000", false) // excess 6000 > current 5000

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1, "resulting magnitude exceeds the current position's, so this is not de-risking")
}

func TestMaxPositionQuantityRuleAtExactLimitPasses(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("1000"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations, "exactly at the limit must be allowed")
}

func TestMaxPositionQuantityRulePropagatesCancelledContext(t *testing.T) {
	r, err := NewMaxPositionQuantityRule(num.MustParseQuantity("1000"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = r.Evaluate(ctx, Input{Proposal: proposal, Account: acc})
	require.ErrorIs(t, err, context.Canceled)
}
