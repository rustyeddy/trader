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

func TestFixedFractionSizerSimpleFX(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")

	// risk budget = 10000 * 0.01 = 100 USD; loss/unit = 0.00500 * 1 =
	// 0.00500; raw = 100 / 0.00500 = 20000; already a whole unit.
	got, err := s.Size(context.Background(), SizeInput{
		Account:      acc,
		Listing:      listing,
		RiskFraction: num.MustParseRate("0.01"),
		StopDistance: num.MustParsePrice("0.00500"),
	})
	require.NoError(t, err)
	assert.True(t, got.Equal(num.MustParseQuantity("20000")), "got %s", got)
}

func TestFixedFractionSizerAppliesContractMultiplier(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "50", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "100000")

	// risk budget = 100000 * 0.02 = 2000; loss/unit = 2 * 50 = 100;
	// raw = 2000 / 100 = 20.
	got, err := s.Size(context.Background(), SizeInput{
		Account:      acc,
		Listing:      listing,
		RiskFraction: num.MustParseRate("0.02"),
		StopDistance: num.MustParsePrice("2"),
	})
	require.NoError(t, err)
	assert.True(t, got.Equal(num.MustParseQuantity("20")), "got %s", got)
}

// TestFixedFractionSizerRoundingNeverExceedsBudget is the property
// this issue's own acceptance criteria center on: the actual implied
// risk of the rounded quantity (quantity * loss-per-unit) must never
// exceed the configured risk budget, even when the raw, unrounded
// quantity is not itself a whole multiple of the listing's increment.
func TestFixedFractionSizerRoundingNeverExceedsBudget(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "100", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "100000")

	riskFraction := num.MustParseRate("0.01")
	stopDistance := num.MustParsePrice("0.03")

	got, err := s.Size(context.Background(), SizeInput{
		Account:      acc,
		Listing:      listing,
		RiskFraction: riskFraction,
		StopDistance: stopDistance,
	})
	require.NoError(t, err)

	// raw = 1000 / 0.03 = 33333.33...; floored to the 100 increment =
	// 33300, not 33333.33 rounded to 33300 by chance -- it must be an
	// exact multiple of the increment, and its implied risk must not
	// exceed the 1000 USD budget.
	assert.True(t, got.Equal(num.MustParseQuantity("33300")), "got %s", got)

	divisible, err := got.DivisibleBy(listing.Spec().QuantityIncrement())
	require.NoError(t, err)
	assert.True(t, divisible)

	impliedRisk, err := stopDistance.MulQuantity(got, num.MustParseCurrency("USD"))
	require.NoError(t, err)
	budget, err := acc.Equity().MulRate(riskFraction)
	require.NoError(t, err)
	cmp, err := impliedRisk.Cmp(budget)
	require.NoError(t, err)
	assert.LessOrEqual(t, cmp, 0, "implied risk %s must not exceed budget %s", impliedRisk, budget)
}

func TestFixedFractionSizerSmallAccountRoundsToZero(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "100")

	// risk budget = 100 * 0.01 = 1 USD; loss/unit = 1000; raw =
	// 0.001, floors to 0 at a whole-unit increment.
	_, err := s.Size(context.Background(), SizeInput{
		Account:      acc,
		Listing:      listing,
		RiskFraction: num.MustParseRate("0.01"),
		StopDistance: num.MustParsePrice("1000"),
	})
	require.ErrorIs(t, err, ErrSizeRoundsToZero)
}

func TestFixedFractionSizerRejectsZeroRiskFraction(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)

	_, err := s.Size(context.Background(), SizeInput{
		Account: acc, Listing: listing,
		RiskFraction: num.Rate{}, StopDistance: num.MustParsePrice("0.005"),
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

func TestFixedFractionSizerRejectsNegativeRiskFraction(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)

	neg, err := num.MustParseRate("0.01").Neg()
	require.NoError(t, err)

	_, err = s.Size(context.Background(), SizeInput{
		Account: acc, Listing: listing,
		RiskFraction: neg, StopDistance: num.MustParsePrice("0.005"),
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

func TestFixedFractionSizerRejectsZeroStopDistance(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)

	_, err := s.Size(context.Background(), SizeInput{
		Account: acc, Listing: listing,
		RiskFraction: num.MustParseRate("0.01"), StopDistance: num.Price{},
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

func TestFixedFractionSizerRejectsUnconstructedAccount(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustEurUsdListing(t)

	_, err := s.Size(context.Background(), SizeInput{
		Listing:      listing,
		RiskFraction: num.MustParseRate("0.01"), StopDistance: num.MustParsePrice("0.005"),
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

func TestFixedFractionSizerRejectsUnconstructedListing(t *testing.T) {
	s := NewFixedFractionSizer()
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, mustEurUsdListing(t))

	_, err := s.Size(context.Background(), SizeInput{
		Account:      acc,
		RiskFraction: num.MustParseRate("0.01"), StopDistance: num.MustParsePrice("0.005"),
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

// TestFixedFractionSizerRejectsSettlementCurrencyMismatch is the
// second correction from review on #181: a USD account must not size
// a listing settled in a different currency without explicit
// conversion.
func TestFixedFractionSizerRejectsSettlementCurrencyMismatch(t *testing.T) {
	s := NewFixedFractionSizer()
	eurSettled := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "EUR")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")

	_, err := s.Size(context.Background(), SizeInput{
		Account:      acc,
		Listing:      eurSettled,
		RiskFraction: num.MustParseRate("0.01"),
		StopDistance: num.MustParsePrice("0.005"),
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

// TestFixedFractionSizerRejectsListingProviderAccountBrokerMismatch is
// a regression for review feedback on PR #196: even when settlement
// and account currencies match, sizing a venue listing against an
// account at a *different* broker is a domain-invalid pairing, the
// same check execution.Planner and risk.Engine already enforce.
func TestFixedFractionSizerRejectsListingProviderAccountBrokerMismatch(t *testing.T) {
	s := NewFixedFractionSizer()
	otherBrokerListing := mustListingWithSpec(t, "alpaca", "0.00001", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")

	_, err := s.Size(context.Background(), SizeInput{
		Account:      acc,
		Listing:      otherBrokerListing,
		RiskFraction: num.MustParseRate("0.01"),
		StopDistance: num.MustParsePrice("0.005"),
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

// TestFixedFractionSizerRoundingNeverExceedsBudgetAtRoundingMidpoint
// is Copilot's own regression from review on PR #196: Money.DivPrice
// rounds to nearest (ties-to-even) at Quantity's smallest
// representable step before RoundDown ever runs. Budget 0.00000003 /
// lossPerUnit 2 = 0.000000015 exactly, the midpoint between
// 0.00000001 and 0.00000002; the even neighbour is 0.00000002, so
// DivPrice rounds up. At a 0.00000001 quantity increment, that value
// is already an exact multiple, so naive RoundDown alone would return
// it unchanged -- implying risk (2 x 0.00000002 = 0.00000004) greater
// than the 0.00000003 budget. Size must correct for this.
func TestFixedFractionSizerRoundingNeverExceedsBudgetAtRoundingMidpoint(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustListingWithSpec(t, "sim", "0.00000001", "0.00000001", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "0.00000003")

	got, err := s.Size(context.Background(), SizeInput{
		Account:      acc,
		Listing:      listing,
		RiskFraction: num.MustParseRate("1"),
		StopDistance: num.MustParsePrice("2"),
	})
	require.NoError(t, err)

	impliedRisk, err := num.MustParsePrice("2").MulQuantity(got, num.MustParseCurrency("USD"))
	require.NoError(t, err)
	budget := acc.Equity()
	cmp, err := impliedRisk.Cmp(budget)
	require.NoError(t, err)
	assert.LessOrEqual(t, cmp, 0, "implied risk %s must not exceed budget %s (got quantity %s)", impliedRisk, budget, got)
	assert.True(t, got.Equal(num.MustParseQuantity("0.00000001")), "got %s", got)
}

func TestFixedFractionSizerPropagatesCancelledContext(t *testing.T) {
	s := NewFixedFractionSizer()
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Size(ctx, SizeInput{
		Account: acc, Listing: listing,
		RiskFraction: num.MustParseRate("0.01"), StopDistance: num.MustParsePrice("0.005"),
	})
	require.ErrorIs(t, err, context.Canceled)
}

// TestFixedFractionSizerDeterministic proves identical inputs produce
// an identical result across independent calls, per this issue's own
// acceptance criteria.
func TestFixedFractionSizerDeterministic(t *testing.T) {
	build := func() (Sizer, SizeInput) {
		listing := mustListingWithSpec(t, "sim", "0.00001", "1", "1", "USD")
		accountID := mustAccountID(t)
		acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
		return NewFixedFractionSizer(), SizeInput{
			Account:      acc,
			Listing:      listing,
			RiskFraction: num.MustParseRate("0.01"),
			StopDistance: num.MustParsePrice("0.00500"),
		}
	}

	s1, in1 := build()
	got1, err := s1.Size(context.Background(), in1)
	require.NoError(t, err)

	s2, in2 := build()
	got2, err := s2.Size(context.Background(), in2)
	require.NoError(t, err)

	assert.True(t, got1.Equal(got2))
}

// TestFullNotionalSizerFullyInvestsEquityAtReferencePrice is
// issue #364/ADR-061's own central acceptance criterion: equity ÷
// reference price, rounded down to the listing's quantity increment.
func TestFullNotionalSizerFullyInvestsEquityAtReferencePrice(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")

	// 10000 / 100.00 = 100 shares exactly.
	ref := num.MustParsePrice("100.00")
	got, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.NoError(t, err)
	assert.True(t, got.Equal(num.MustParseQuantity("100")), "got %s", got)
}

func TestFullNotionalSizerAppliesContractMultiplier(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "50", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "100000")

	// cost/unit = 40 * 50 = 2000; 100000 / 2000 = 50.
	ref := num.MustParsePrice("40")
	got, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.NoError(t, err)
	assert.True(t, got.Equal(num.MustParseQuantity("50")), "got %s", got)
}

// TestFullNotionalSizerRoundingNeverExceedsBudget mirrors
// TestFixedFractionSizerRoundingNeverExceedsBudget: the actual implied
// cost of the rounded quantity must never exceed available capital,
// even when the raw quantity is not itself a whole multiple of the
// listing's increment.
func TestFullNotionalSizerRoundingNeverExceedsBudget(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "100", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "100000")

	ref := num.MustParsePrice("3.00")
	got, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.NoError(t, err)

	// raw = 100000 / 3.00 = 33333.33...; floored to the 100 increment
	// = 33300.
	assert.True(t, got.Equal(num.MustParseQuantity("33300")), "got %s", got)

	divisible, err := got.DivisibleBy(listing.Spec().QuantityIncrement())
	require.NoError(t, err)
	assert.True(t, divisible)

	impliedCost, err := ref.MulQuantity(got, num.MustParseCurrency("USD"))
	require.NoError(t, err)
	cmp, err := impliedCost.Cmp(acc.Equity())
	require.NoError(t, err)
	assert.LessOrEqual(t, cmp, 0, "implied cost %s must not exceed equity %s", impliedCost, acc.Equity())
}

func TestFullNotionalSizerSmallAccountRoundsToZero(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "50")

	ref := num.MustParsePrice("100")
	_, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.ErrorIs(t, err, ErrSizeRoundsToZero)
}

func TestFullNotionalSizerRejectsNilReferencePrice(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)

	_, err := s.Size(context.Background(), SizeInput{
		Account: acc,
		Listing: listing,
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

func TestFullNotionalSizerRejectsZeroReferencePrice(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	zero := num.Price{}

	_, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &zero,
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

func TestFullNotionalSizerRejectsUnconstructedAccount(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustEurUsdListing(t)
	ref := num.MustParsePrice("1.10000")

	_, err := s.Size(context.Background(), SizeInput{
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

func TestFullNotionalSizerRejectsSettlementCurrencyMismatch(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "1", "EUR")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
	ref := num.MustParsePrice("1.10000")

	_, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.ErrorIs(t, err, ErrInvalidSizeInput)
}

// TestFullNotionalSizerUsesLesserOfEquityAndBuyingPower is issue
// #364/ADR-061 review's own required regression: Equity() alone
// already includes any existing position's value, so sizing a *new*
// position from Equity() alone could propose spending more than the
// broker actually has available. min(Equity, BuyingPower) must be
// used instead — here BuyingPower is deliberately lower than Equity.
func TestFullNotionalSizerUsesLesserOfEquityAndBuyingPower(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquityAndBuyingPower(t, accountID, "sim", "USD", "10000", "4000")

	// BuyingPower (4000) is the binding constraint, not Equity
	// (10000): 4000 / 100.00 = 40 shares, not 100.
	ref := num.MustParsePrice("100.00")
	got, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.NoError(t, err)
	assert.True(t, got.Equal(num.MustParseQuantity("40")), "got %s", got)
}

// TestFullNotionalSizerUsesEquityWhenBuyingPowerIsHigher proves the
// min() is genuinely symmetric: when BuyingPower exceeds Equity (for
// example margin availability), Equity remains the binding "never
// more than 100% of equity" constraint.
func TestFullNotionalSizerUsesEquityWhenBuyingPowerIsHigher(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquityAndBuyingPower(t, accountID, "sim", "USD", "10000", "20000")

	// Equity (10000) is the binding constraint, not BuyingPower
	// (20000): 10000 / 100.00 = 100 shares, not 200.
	ref := num.MustParsePrice("100.00")
	got, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.NoError(t, err)
	assert.True(t, got.Equal(num.MustParseQuantity("100")), "got %s", got)
}

// mustGbpUsdListingWithSpec is mustListingWithSpec's own identical
// pattern for a second, genuinely different instrument (GBP/USD
// rather than EUR/USD) — needed to prove fullNotionalSizer's flat-only
// guard is account-wide, not merely scoped to the instrument being
// sized (PR #371 re-review): mustListingWithSpec always builds EUR/USD
// regardless of its own provider/spec parameters, so it cannot itself
// produce a second, distinct instrument identity.
func mustGbpUsdListingWithSpec(t *testing.T, provider, tickSize, quantityIncrement, multiplier, settlementCurrency string) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice(tickSize),
		num.MustParseQuantity(quantityIncrement),
		num.MustParseRate(multiplier),
		num.MustParseCurrency(settlementCurrency),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   provider,
		Symbol:     "GBP_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// TestFullNotionalSizerRejectsExistingPositionInSameInstrument is PR
// #371 review's own required blocker regression: order.IntentEnter
// means "open or increase," but fullNotionalSizer always computes a
// brand-new order equal to its entire available budget. Sizing a
// second such order on top of an already-open position (same
// instrument, either side) must be rejected outright rather than
// silently risking total exposure past 100% of equity.
func TestFullNotionalSizerRejectsExistingPositionInSameInstrument(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
	accountID := mustAccountID(t)
	existing := mustPosition(t, accountID, listing, order.Long, "10")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", existing)
	ref := num.MustParsePrice("100.00")

	_, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.ErrorIs(t, err, ErrFullNotionalRequiresFlat)
}

// TestFullNotionalSizerRejectsExistingPositionInDifferentInstrument is
// PR #371 re-review's own required regression: the flat-only guard is
// account-wide, not merely scoped to the instrument being sized. An
// existing position in a *different* instrument (GBP/USD) must still
// reject sizing a brand-new full-notional order in EUR/USD — an
// instrument-scoped check alone would incorrectly allow this, letting
// a second full-equity-notional order stack on top of existing
// exposure (for example on a leveraged account where BuyingPower() >=
// Equity()).
func TestFullNotionalSizerRejectsExistingPositionInDifferentInstrument(t *testing.T) {
	s := NewFullNotionalSizer()
	target := mustListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
	other := mustGbpUsdListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
	accountID := mustAccountID(t)
	existing := mustPosition(t, accountID, other, order.Long, "10")
	acc := mustSnapshotWithPositions(t, accountID, "sim", "USD", "10000", existing)
	ref := num.MustParsePrice("100.00")

	_, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        target,
		ReferencePrice: &ref,
	})
	require.ErrorIs(t, err, ErrFullNotionalRequiresFlat)
}

// TestFullNotionalSizerRoundingNeverExceedsBudgetAtRoundingMidpoint
// mirrors TestFixedFractionSizerRoundingNeverExceedsBudgetAtRoundingMidpoint
// exactly: forces DivPrice's own nearest-rounding to push the raw
// quantity to precisely the midpoint between two representable
// quantity steps, proving the post-rounding correction loop actually
// engages rather than merely existing unexercised (PR #371 review).
func TestFullNotionalSizerRoundingNeverExceedsBudgetAtRoundingMidpoint(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustListingWithSpec(t, "sim", "0.00000001", "0.00000001", "1", "USD")
	accountID := mustAccountID(t)
	acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "0.00000003")
	ref := num.MustParsePrice("2")

	got, err := s.Size(context.Background(), SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.NoError(t, err)

	impliedCost, err := ref.MulQuantity(got, num.MustParseCurrency("USD"))
	require.NoError(t, err)
	budget := acc.Equity()
	cmp, err := impliedCost.Cmp(budget)
	require.NoError(t, err)
	assert.LessOrEqual(t, cmp, 0, "implied cost %s must not exceed budget %s (got quantity %s)", impliedCost, budget, got)
	assert.True(t, got.Equal(num.MustParseQuantity("0.00000001")), "got %s", got)
}

func TestFullNotionalSizerPropagatesCancelledContext(t *testing.T) {
	s := NewFullNotionalSizer()
	listing := mustEurUsdListing(t)
	accountID := mustAccountID(t)
	acc := mustSnapshot(t, accountID, listing)
	ref := num.MustParsePrice("1.10000")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Size(ctx, SizeInput{
		Account:        acc,
		Listing:        listing,
		ReferencePrice: &ref,
	})
	require.ErrorIs(t, err, context.Canceled)
}

// TestFullNotionalSizerDeterministic proves two independent calls
// against identical input produce an identical result.
func TestFullNotionalSizerDeterministic(t *testing.T) {
	build := func() (Sizer, SizeInput) {
		listing := mustListingWithSpec(t, "sim", "0.01", "1", "1", "USD")
		accountID := mustAccountID(t)
		acc := mustSnapshotWithEquity(t, accountID, "sim", "USD", "10000")
		ref := num.MustParsePrice("100.00")
		return NewFullNotionalSizer(), SizeInput{
			Account:        acc,
			Listing:        listing,
			ReferencePrice: &ref,
		}
	}

	s1, in1 := build()
	got1, err := s1.Size(context.Background(), in1)
	require.NoError(t, err)

	s2, in2 := build()
	got2, err := s2.Size(context.Background(), in2)
	require.NoError(t, err)

	assert.True(t, got1.Equal(got2))
}
