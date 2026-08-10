package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustWorkingOrder(t *testing.T) order.Order {
	t.Helper()
	g := testGenerator()
	listing := tradertest.MustNewListing(tradertest.ListingParams{})
	proposal := tradertest.MustNewProposal(tradertest.ProposalParams{
		Listing:   listing,
		AccountID: tradertest.MustAccountID(g),
	})
	request, err := order.NewRequest(proposal, tradertest.MustOrderID(g))
	require.NoError(t, err)
	return tradertest.MustNewOrder(tradertest.OrderParams{Request: request})
}

func TestNewFillForDefaults(t *testing.T) {
	g := testGenerator()
	o := mustWorkingOrder(t)

	f, err := tradertest.NewFillFor(tradertest.FillParams{
		Order:  o,
		FillID: tradertest.MustFillID(g),
	})
	require.NoError(t, err)

	assert.Equal(t, o.Request.OrderID, f.OrderID)
	assert.Equal(t, o.Request.AccountID, f.AccountID)
	remaining, err := o.RemainingQuantity()
	require.NoError(t, err)
	assert.True(t, f.Quantity.Equal(remaining))
}

func TestNewFillForPartialQuantity(t *testing.T) {
	g := testGenerator()
	o := mustWorkingOrder(t)

	f, err := tradertest.NewFillFor(tradertest.FillParams{
		Order:    o,
		FillID:   tradertest.MustFillID(g),
		Quantity: "400",
	})
	require.NoError(t, err)

	applied, err := order.ApplyFill(o, f)
	require.NoError(t, err)
	assert.Equal(t, order.StatusPartiallyFilled, applied.Status)
}

func TestNewFillForExplicitPrice(t *testing.T) {
	g := testGenerator()
	o := mustWorkingOrder(t)

	f, err := tradertest.NewFillFor(tradertest.FillParams{
		Order:        o,
		FillID:       tradertest.MustFillID(g),
		Price:        "1.23456",
		BrokerFillID: "broker-fill-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "broker-fill-1", f.BrokerFillID)
}

func TestNewFillForAppliesCleanly(t *testing.T) {
	g := testGenerator()
	o := mustWorkingOrder(t)

	f := tradertest.MustNewFillFor(tradertest.FillParams{
		Order:  o,
		FillID: tradertest.MustFillID(g),
	})

	filled, err := order.ApplyFill(o, f)
	require.NoError(t, err)
	assert.Equal(t, order.StatusFilled, filled.Status)
}

func TestMustNewFillForPanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		tradertest.MustNewFillFor(tradertest.FillParams{})
	})
}
