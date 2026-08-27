package risk

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/num"
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
