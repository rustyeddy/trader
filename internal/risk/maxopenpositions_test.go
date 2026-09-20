package risk

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMaxOpenPositionsRuleRejectsNonPositive(t *testing.T) {
	_, err := NewMaxOpenPositionsRule(0)
	require.ErrorIs(t, err, ErrInvalidRule)

	_, err = NewMaxOpenPositionsRule(-1)
	require.ErrorIs(t, err, ErrInvalidRule)
}

func TestMaxOpenPositionsRuleName(t *testing.T) {
	r, err := NewMaxOpenPositionsRule(5)
	require.NoError(t, err)
	assert.Equal(t, "max_open_positions", r.Name())
}

// TestMaxOpenPositionsRuleFlatToOpenIncrementsCount: opening the
// account's first position, under the limit, passes.
func TestMaxOpenPositionsRuleFlatToOpenWithinLimit(t *testing.T) {
	r, err := NewMaxOpenPositionsRule(1)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestMaxOpenPositionsRuleFlatToOpenExceedsLimit: the account already
// holds one position (at the limit); opening a second, different
// instrument violates.
func TestMaxOpenPositionsRuleFlatToOpenExceedsLimit(t *testing.T) {
	r, err := NewMaxOpenPositionsRule(1)
	require.NoError(t, err)

	eurUsd := mustEurUsdListing(t)
	gbpUsd := mustGbpUsdListing(t)
	accountID := mustAccountID(t)
	existing := mustPosition(t, accountID, eurUsd, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", existing)
	proposal := mustProposalWith(t, accountID, gbpUsd, order.Buy, "500", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "2", result.Violations[0].Measured)
	assert.Equal(t, "1", result.Violations[0].Limit)
}

// TestMaxOpenPositionsRuleAddToExistingNeverBlocked: adding to an
// already-open position doesn't change the count, so it passes
// regardless of the configured limit.
func TestMaxOpenPositionsRuleAddToExistingNeverBlocked(t *testing.T) {
	r, err := NewMaxOpenPositionsRule(1)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "500", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestMaxOpenPositionsRuleNonReduceOnlyExactCloseDecrementsAndPasses
// is the correction from review on #183: a non-ReduceOnly proposal
// that exactly closes a position still reduces the count, and a count
// decrease is never blocked.
func TestMaxOpenPositionsRuleNonReduceOnlyExactCloseDecrementsAndPasses(t *testing.T) {
	r, err := NewMaxOpenPositionsRule(1)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "500", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestMaxOpenPositionsRuleReversalCountUnchanged is the correction
// from review on #183: a reversal crosses Flat but ends with exactly
// one position in the same instrument -- the count does not change,
// so it is never blocked by this rule regardless of the limit.
func TestMaxOpenPositionsRuleReversalCountUnchanged(t *testing.T) {
	r, err := NewMaxOpenPositionsRule(1)
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "100")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "150", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

func TestMaxOpenPositionsRuleAtExactLimitPasses(t *testing.T) {
	r, err := NewMaxOpenPositionsRule(2)
	require.NoError(t, err)

	eurUsd := mustEurUsdListing(t)
	gbpUsd := mustGbpUsdListing(t)
	accountID := mustAccountID(t)
	existing := mustPosition(t, accountID, eurUsd, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", existing)
	proposal := mustProposalWith(t, accountID, gbpUsd, order.Buy, "500", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations, "reaching exactly the limit must be allowed")
}

func TestMaxOpenPositionsRulePropagatesCancelledContext(t *testing.T) {
	r, err := NewMaxOpenPositionsRule(1)
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
