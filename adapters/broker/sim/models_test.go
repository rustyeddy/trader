package sim

import (
	"context"
	"fmt"
	"testing"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedSlippage is a deterministic SlippageModel that always returns
// the same configured price, regardless of the base price it is
// given — enough to prove substitution changes execution output
// without needing a realistic model.
type fixedSlippage struct {
	price num.Price
}

func (f fixedSlippage) Info() ModelInfo {
	return ModelInfo{Name: "fixedSlippage", Version: "test", Config: "price=" + f.price.String()}
}

func (f fixedSlippage) Slippage(listing instrument.Listing, side order.Side, quantity num.Quantity, price num.Price) (num.Price, error) {
	return f.price, nil
}

// erroringSlippage always fails, for atomicity tests.
type erroringSlippage struct{}

func (erroringSlippage) Info() ModelInfo { return ModelInfo{Name: "erroringSlippage", Version: "test"} }
func (erroringSlippage) Slippage(listing instrument.Listing, side order.Side, quantity num.Quantity, price num.Price) (num.Price, error) {
	return num.Price{}, fmt.Errorf("erroringSlippage: always fails")
}

// fixedCommission is a deterministic CommissionModel charging a flat
// amount per fill, in the given currency.
type fixedCommission struct {
	amount num.Money
}

func (f fixedCommission) Info() ModelInfo {
	return ModelInfo{Name: "fixedCommission", Version: "test", Config: "amount=" + f.amount.String()}
}

func (f fixedCommission) Commission(listing instrument.Listing, side order.Side, quantity num.Quantity, price num.Price) (*num.Money, error) {
	amount := f.amount
	return &amount, nil
}

// pricedCommission is a CommissionModel that records the price it was
// invoked with, to prove commission is computed from the final
// (post-slippage) price rather than the pre-slippage base.
type pricedCommission struct {
	seenPrice *num.Price
}

func (p *pricedCommission) Info() ModelInfo {
	return ModelInfo{Name: "pricedCommission", Version: "test"}
}
func (p *pricedCommission) Commission(listing instrument.Listing, side order.Side, quantity num.Quantity, price num.Price) (*num.Money, error) {
	p.seenPrice = &price
	zero := num.MustParseMoney("0", listing.Spec().SettlementCurrency())
	return &zero, nil
}

func TestBuildFillDefaultsToExactPriceAndNoFee(t *testing.T) {
	ctx := context.Background()
	deps := testDeps() // no Slippage/Commission configured
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	o, err := acc.Submit(ctx, req)
	require.NoError(t, err)

	require.Len(t, o.AppliedFillIDs, 1)
	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.NotNil(t, snap.Positions()[0].AvgPrice)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.10000")), "exact base price, no slippage applied")
	assert.True(t, snap.Fees().Equal(usd("0")))
	assert.True(t, snap.Equity().Equal(usd("10000")), "no commission charged")
}

func TestBuildFillAppliesSlippageToMarketOrder(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	deps.Slippage = fixedSlippage{price: num.MustParsePrice("1.10050")}
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.NotNil(t, snap.Positions()[0].AvgPrice)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.10050")), "the slipped price, not the base 1.10000, becomes the position's entry price")
}

func TestBuildFillAppliesSlippageToTriggeredStop(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	deps.Slippage = fixedSlippage{price: num.MustParsePrice("1.10150")}
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustStopRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09500", "1.10200", "1.09400", "1.10100", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.NotNil(t, snap.Positions()[0].AvgPrice)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.10150")), "slippage applies after the stop's own trigger/gap price is resolved")
}

func TestBuildFillNeverAppliesSlippageToLimitOrder(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	deps.Slippage = fixedSlippage{price: num.MustParsePrice("1.20000")} // would be very obviously wrong if applied
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09900", "1.10100", "1.09700", "1.09950", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.NotNil(t, snap.Positions()[0].AvgPrice)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.09900")), "a limit fill is a price guarantee; slippage must never move it")
}

func TestBuildFillAppliesCommissionToCashAndFees(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	deps.Commission = fixedCommission{amount: usd("2.50")}
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.True(t, snap.Fees().Equal(usd("2.50")))
	assert.True(t, snap.Equity().Equal(usd("9997.50")))
	assert.True(t, snap.CashBalances()[0].Equal(usd("9997.50")))
}

func TestBuildFillComputesCommissionFromFinalSlippedPrice(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	deps.Slippage = fixedSlippage{price: num.MustParsePrice("1.10050")}
	commission := &pricedCommission{}
	deps.Commission = commission
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	require.NotNil(t, commission.seenPrice)
	assert.True(t, commission.seenPrice.Equal(num.MustParsePrice("1.10050")), "commission must see the slipped price, not the pre-slippage base 1.10000")
}

// TestBuildFillRejectsInvalidSlippageAdjustedPrice covers the review
// finding that a SlippageModel's returned price must be validated
// immediately — before Commission is ever consulted, and before any
// state commits — not left to order.NewFill's later tick-size check to
// catch after Commission has already run against an invalid price.
func TestBuildFillRejectsInvalidSlippageAdjustedPrice(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	// mustEurUsdListing's tick size is 0.00001; this price is not an
	// exact multiple of it.
	deps.Slippage = fixedSlippage{price: num.MustParsePrice("1.100001")}
	commission := &pricedCommission{}
	deps.Commission = commission
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.ErrorIs(t, err, order.ErrInvalidFill)
	assert.Nil(t, commission.seenPrice, "commission must never be consulted with an invalid slippage-adjusted price")

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions())
	assert.Empty(t, snap.OpenOrders())
	assert.True(t, snap.Equity().Equal(usd("10000")))

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	assertNoMoreEventsSoon(t, reader)
}

// TestBuildFillSlippageErrorLeavesNoState covers atomicity: a failing
// model must leave every part of state exactly as it was, matching
// #149/#152's established build-then-commit discipline.
func TestBuildFillSlippageErrorLeavesNoState(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	deps.Slippage = erroringSlippage{}
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, req)
	require.Error(t, err)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Positions())
	assert.Empty(t, snap.OpenOrders())
	assert.True(t, snap.Equity().Equal(usd("10000")))

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	assertNoMoreEventsSoon(t, reader) // not even the order-accepted event survives
}
