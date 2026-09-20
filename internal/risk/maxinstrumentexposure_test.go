package risk

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func usd(t *testing.T, s string) num.Money {
	t.Helper()
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
}

func TestNewMaxInstrumentExposureRuleRejectsInvalidMoney(t *testing.T) {
	_, err := NewMaxInstrumentExposureRule(num.Money{})
	require.ErrorIs(t, err, ErrInvalidRule)
}

func TestNewMaxInstrumentExposureRuleRejectsZero(t *testing.T) {
	_, err := NewMaxInstrumentExposureRule(usd(t, "0"))
	require.ErrorIs(t, err, ErrInvalidRule)
}

// TestNewMaxInstrumentExposureRuleRejectsNegative is a regression for
// Copilot's review on PR #198: num.Money is signed, so the prior
// !IsZero() check alone let a negative max through, silently causing
// every non-flat resulting position to violate the rule.
func TestNewMaxInstrumentExposureRuleRejectsNegative(t *testing.T) {
	neg, err := usd(t, "100").Neg()
	require.NoError(t, err)
	_, err = NewMaxInstrumentExposureRule(neg)
	require.ErrorIs(t, err, ErrInvalidRule)
}

func TestMaxInstrumentExposureRuleName(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "10000"))
	require.NoError(t, err)
	assert.Equal(t, "max_instrument_exposure", r.Name())
}

func TestMaxInstrumentExposureRuleWithinLimitPasses(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "2000"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	ref := num.MustParsePrice("1.10000") // exposure = 1100, within 2000

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

func TestMaxInstrumentExposureRuleExceedsLimitViolates(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "1000"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	ref := num.MustParsePrice("1.10000") // exposure = 1100 > 1000

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "1100 USD", result.Violations[0].Measured)
	assert.Equal(t, "1000 USD", result.Violations[0].Limit)
}

// TestMaxInstrumentExposureRuleValuesEntireResultingQuantityAtReferencePrice
// is the correction from review on #183: the held portion of a
// position must not be valued at its own historical AvgPrice while
// only the delta uses ReferencePrice -- the entire resulting quantity
// is valued at ReferencePrice alone.
func TestMaxInstrumentExposureRuleValuesEntireResultingQuantityAtReferencePrice(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "2200"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	// Existing position opened at a very different historical price --
	// if AvgPrice leaked into the calculation, the result would differ
	// from "entire resulting quantity x ReferencePrice".
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "500", false)
	ref := num.MustParsePrice("2") // resulting qty 1000 x 2 = 2000, within 2200

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

func TestMaxInstrumentExposureRuleAppliesContractMultiplier(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "1000"))
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.01", "1", "50", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1", false)
	ref := num.MustParsePrice("21") // value/unit = 21*50=1050 > 1000 limit

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1)
}

// TestMaxInstrumentExposureRuleReduceOnlyRequiresNoReferencePrice
// proves a reduce-only proposal that flattens the position never
// needs a ReferencePrice at all -- there's no resulting exposure to
// value.
func TestMaxInstrumentExposureRuleReduceOnlyToFlatRequiresNoReferencePrice(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "100"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "500")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "500", true)

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestMaxInstrumentExposureRuleReduceOnlyDeRiskingAlwaysPasses is
// issue #207's own regression, mirroring MaxPositionLeverageRule's
// identical fix (#184/PR #199, ADR-034): an account already over the
// configured cap must not be trapped there by having a genuinely
// risk-reducing move rejected merely because the *resulting* exposure
// is still over the cap. Existing 600000 at ReferencePrice 1 already
// exceeds the 100 cap; reducing by 50000 to 550000 is still over the
// cap in isolation, but the reduction itself must be allowed.
func TestMaxInstrumentExposureRuleReduceOnlyDeRiskingAlwaysPasses(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "100"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "600000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "50000", true)
	ref := num.MustParsePrice("1") // resulting qty 550000; exposure 550000, still > 100 cap

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	assert.Empty(t, result.Violations, "a de-risking move must never be blocked, even if the resulting exposure remains over the cap")
}

// TestMaxInstrumentExposureRuleNonReduceOnlyPartialDeRiskingAlwaysPasses
// is the same invariant as the ReduceOnly case above, for a
// non-ReduceOnly proposal that is still a strict partial reduction.
func TestMaxInstrumentExposureRuleNonReduceOnlyPartialDeRiskingAlwaysPasses(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "100"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "600000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "50000", false)
	ref := num.MustParsePrice("1")

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	assert.Empty(t, result.Violations)
}

// TestMaxInstrumentExposureRuleReversalPastCurrentMagnitudeIsEvaluatedNormally
// covers the boundary of the de-risking exemption itself: starting
// from an already over-limit position, a reversal whose *resulting*
// magnitude exceeds the current position's is not de-risking — it is
// evaluated against the cap exactly as any other increase would be.
func TestMaxInstrumentExposureRuleReversalPastCurrentMagnitudeIsEvaluatedNormally(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "100"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	pos := mustPosition(t, accountID, listing, order.Long, "600000")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", pos)
	proposal := mustProposalWith(t, accountID, listing, order.Sell, "1300000", false) // excess 700000 > current 600000
	ref := num.MustParsePrice("1")

	result, err := r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.NoError(t, err)
	require.Len(t, result.Violations, 1, "resulting magnitude exceeds the current position's, so this is not de-risking")
}

func TestMaxInstrumentExposureRuleRequiresReferencePriceForOpenExposure(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "1000"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc})
	require.ErrorIs(t, err, ErrInsufficientRuleInput)
}

func TestMaxInstrumentExposureRuleRejectsZeroReferencePrice(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "1000"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	zero := num.Price{}

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &zero})
	require.ErrorIs(t, err, ErrInsufficientRuleInput)
}

func TestMaxInstrumentExposureRuleRejectsSettlementCurrencyMismatch(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "1000"))
	require.NoError(t, err)

	eurSettled := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "EUR")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, eurSettled, order.Buy, "1000", false)
	ref := num.MustParsePrice("1.1")

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.ErrorIs(t, err, ErrInsufficientRuleInput)
}

// TestMaxInstrumentExposureRuleRejectsConfiguredLimitCurrencyMismatch
// is a regression for Rusty's review on PR #198: a rule configured
// with a EUR limit but evaluated against a USD account/listing (both
// internally consistent with each other) previously reached
// exposure.Cmp(r.max) and surfaced a raw num currency-mismatch
// arithmetic error instead of a classifiable rule-input error.
func TestMaxInstrumentExposureRuleRejectsConfiguredLimitCurrencyMismatch(t *testing.T) {
	eurLimit := num.MustParseMoney("1000", num.MustParseCurrency("EUR"))
	r, err := NewMaxInstrumentExposureRule(eurLimit)
	require.NoError(t, err)

	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	proposal := mustProposalWith(t, accountID, listing, order.Buy, "1000", false)
	ref := num.MustParsePrice("1.1")

	_, err = r.Evaluate(context.Background(), Input{Proposal: proposal, Account: acc, ReferencePrice: &ref})
	require.ErrorIs(t, err, ErrInsufficientRuleInput)
}

func TestMaxInstrumentExposureRulePropagatesCancelledContext(t *testing.T) {
	r, err := NewMaxInstrumentExposureRule(usd(t, "1000"))
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
