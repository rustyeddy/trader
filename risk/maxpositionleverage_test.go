package risk

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMaxPositionLeverageRuleRejectsZero(t *testing.T) {
	_, err := NewMaxPositionLeverageRule(num.Rate{})
	require.ErrorIs(t, err, ErrInvalidRule)
}

func TestNewMaxPositionLeverageRuleRejectsNegative(t *testing.T) {
	neg, err := num.MustParseRate("50").Neg()
	require.NoError(t, err)
	_, err = NewMaxPositionLeverageRule(neg)
	require.ErrorIs(t, err, ErrInvalidRule)
}

func TestMaxPositionLeverageRuleName(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)
	assert.Equal(t, "max_position_leverage", r.Name())
}

// leverageListing is a EUR/USD listing with multiplier 1, so notional
// value equals ReferencePrice x quantity exactly, keeping every
// boundary calculation in this file a clean decimal.
func leverageListing(t *testing.T) instrument.Listing {
	t.Helper()
	return mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
}

func TestMaxPositionLeverageRuleWithinLimitPasses(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := leverageListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "400000", false)
	ref := num.MustParsePrice("1") // notional 400000; required margin 8000 <= 10000

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

func TestMaxPositionLeverageRuleAtExactLimitPasses(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := leverageListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "500000", false)
	ref := num.MustParsePrice("1") // notional 500000; required margin 10000 == equity exactly

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	assert.Empty(t, result.Violations, "exactly at the limit must be allowed")
}

func TestMaxPositionLeverageRuleOverLimitViolates(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := leverageListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "500001", false)
	ref := num.MustParsePrice("1") // notional 500001; required margin 10000.02 > 10000

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "10000.02 USD", result.Violations[0].Measured)
	assert.Equal(t, "10000 USD", result.Violations[0].Limit)
}

func TestMaxPositionLeverageRuleAppliesContractMultiplier(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.01", "1", "50", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1", false)
	ref := num.MustParsePrice("500001") // notional = 500001*50 = 25000050; margin = 500001 > 10000

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
}

// TestMaxPositionLeverageRuleReduceNeverBlocked proves a ReduceOnly
// proposal that shrinks a much larger existing position passes: the
// resulting position's own required margin is what's evaluated, not
// the existing position's (which may have been entered under a
// different or since-changed configuration).
func TestMaxPositionLeverageRuleReduceNeverBlocked(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := leverageListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "1000000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "500000", true)
	ref := num.MustParsePrice("1") // resulting qty 500000; required margin 10000 == equity

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestMaxPositionLeverageRuleFullCloseRequiresNoReferencePrice proves
// a ReduceOnly exact close needs no ReferencePrice at all -- there is
// no resulting exposure to value.
func TestMaxPositionLeverageRuleFullCloseRequiresNoReferencePrice(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := leverageListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "500000", true)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

func TestMaxPositionLeverageRuleReversalWithinLimitPasses(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := leverageListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "100000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "600000", false)
	ref := num.MustParsePrice("1") // excess 500000; required margin 10000 == equity

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

func TestMaxPositionLeverageRuleReversalOverLimitViolates(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := leverageListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "100000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "600001", false)
	ref := num.MustParsePrice("1") // excess 500001; required margin 10000.02 > 10000

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
}

func TestMaxPositionLeverageRuleRequiresReferencePriceForOpenExposure(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.ErrorIs(t, err, ErrInsufficientRuleInput)
}

func TestMaxPositionLeverageRuleRejectsZeroReferencePrice(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	zero := num.Price{}

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &zero})
	require.ErrorIs(t, err, ErrInsufficientRuleInput)
}

func TestMaxPositionLeverageRuleRejectsSettlementCurrencyMismatch(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	eurSettled := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "EUR")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, eurSettled, order.Buy, "1000", false)
	ref := num.MustParsePrice("1.1")

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.ErrorIs(t, err, ErrInsufficientRuleInput)
}

func TestMaxPositionLeverageRulePropagatesCancelledContext(t *testing.T) {
	r, err := NewMaxPositionLeverageRule(num.MustParseRate("50"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	ref := num.MustParsePrice("1.1")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = r.Evaluate(ctx, Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.ErrorIs(t, err, context.Canceled)
}
