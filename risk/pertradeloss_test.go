package risk

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPerTradeLossRuleRejectsNonPositiveFraction(t *testing.T) {
	_, err := NewPerTradeLossRule(num.Rate{})
	require.ErrorIs(t, err, ErrInvalidRule)

	neg, err := num.MustParseRate("0.01").Neg()
	require.NoError(t, err)
	_, err = NewPerTradeLossRule(neg)
	require.ErrorIs(t, err, ErrInvalidRule)
}

func TestPerTradeLossRuleName(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)
	assert.Equal(t, "per_trade_loss", r.Name())
}

// TestPerTradeLossRuleNewPosition: no existing position, a plain
// (non-ReduceOnly) proposal opens exposure equal to its full quantity.
func TestPerTradeLossRuleNewPosition(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	dist := num.MustParsePrice("0.00050") // loss/unit 0.0005 * 1000 = 0.5, well within 100 budget

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestPerTradeLossRuleAddToSameDirection: existing long position, a
// non-ReduceOnly Buy proposal is fully incremental new exposure.
func TestPerTradeLossRuleAddToSameDirection(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	dist := num.MustParsePrice("0.00500") // budget = 100; loss = 0.005*1000 = 5, within budget

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestPerTradeLossRuleAddToSameDirectionExceedsBudget proves the full
// proposal quantity -- not some smaller delta -- is what gets charged
// when adding to an existing same-direction position.
func TestPerTradeLossRuleAddToSameDirectionExceedsBudget(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	// budget = 100; loss = 1 * 1000 = 1000, far exceeds budget.
	dist := num.MustParsePrice("1")

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "1000 USD", result.Violations[0].Measured)
	assert.Equal(t, "100 USD", result.Violations[0].Limit)
}

// TestPerTradeLossRulePartialReduceOnlyPasses: ReduceOnly always
// passes without requiring AdverseDistance at all -- reducing exposure
// is never charged as new risk.
func TestPerTradeLossRulePartialReduceOnlyPasses(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "200", true)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestPerTradeLossRuleFullExitPasses: a full ReduceOnly close, same as
// the partial case, requires no AdverseDistance and never violates.
func TestPerTradeLossRuleFullExitPasses(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "500", true)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestPerTradeLossRuleNonReduceOnlyExactCloseIsZeroNewRisk covers a
// non-ReduceOnly proposal that exactly closes the existing position
// (opposite direction, equal quantity): no new exposure is created, so
// no AdverseDistance is required and it never violates.
func TestPerTradeLossRuleNonReduceOnlyExactCloseIsZeroNewRisk(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "500", false)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestPerTradeLossRuleReversalThroughFlatChargesOnlyTheExcess is the
// scenario from review on #182: current long 100, proposal sells 150
// (a reversal to short 50). Only the 50-unit excess beyond the
// existing position is genuinely new exposure; the rule must not
// charge the full 150.
func TestPerTradeLossRuleReversalThroughFlatChargesOnlyTheExcess(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "100")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "150", false)

	// budget = 100; distance 1 -> if charged on the full 150, loss =
	// 150 > budget (violation); charged correctly on just the 50-unit
	// excess, loss = 50 <= budget (passes).
	dist := num.MustParsePrice("1")

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.NoError(t, err)
	assert.Empty(t, result.Violations, "only the 50-unit excess beyond the existing 100 should be charged as new risk")
}

func TestPerTradeLossRuleReversalExcessExceedsBudget(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "100")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "150", false)

	// budget = 100; distance 3 -> excess 50 * 3 = 150 > 100 budget.
	dist := num.MustParsePrice("3")

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "150 USD", result.Violations[0].Measured)
	assert.Equal(t, "100 USD", result.Violations[0].Limit)
}

func TestPerTradeLossRuleAppliesContractMultiplier(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.02"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.01", "1", "50", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "100000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "20", false)
	dist := num.MustParsePrice("2") // loss/unit = 2*50=100; loss = 100*20=2000 = budget exactly

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.NoError(t, err)
	assert.Empty(t, result.Violations, "exactly at the budget must be allowed, not rejected")
}

func TestPerTradeLossRuleBoundaryJustAboveLimitViolates(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1001", false)
	// budget = 100; distance 0.1 -> loss = 100.1, just above.
	dist := num.MustParsePrice("0.1")

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
}

func TestPerTradeLossRuleRequiresAdverseDistanceForNewExposure(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.Error(t, err)
}

func TestPerTradeLossRuleRejectsZeroAdverseDistance(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	zero := num.Price{}

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &zero})
	require.Error(t, err)
}

func TestPerTradeLossRuleRejectsSettlementCurrencyMismatch(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	eurSettled := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "EUR")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, eurSettled, order.Buy, "1000", false)
	dist := num.MustParsePrice("0.005")

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.Error(t, err)
}

func TestPerTradeLossRulePropagatesCancelledContext(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	dist := num.MustParsePrice("0.005")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = r.Evaluate(ctx, Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.ErrorIs(t, err, context.Canceled)
}

// TestPerTradeLossRuleThroughEngineNormalizesAttribution proves this
// rule composes correctly through Engine (#180): a violation's Rule
// field is normalized to Name(), independent of the rule's own
// Violations construction (which leaves Rule unset -- Engine's own
// responsibility per #195's review).
func TestPerTradeLossRuleThroughEngineNormalizesAttribution(t *testing.T) {
	r, err := NewPerTradeLossRule(num.MustParseRate("0.01"))
	require.NoError(t, err)
	e, err := NewEngine(r)
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	dist := num.MustParsePrice("1") // loss 1000 >> budget 100

	decision, err := e.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, AdverseDistance: &dist})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	require.Len(t, decision.Violations, 1)
	assert.Equal(t, "per_trade_loss", decision.Violations[0].Rule)
}
