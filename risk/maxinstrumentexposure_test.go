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
