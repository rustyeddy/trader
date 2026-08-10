package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOrderDefaults(t *testing.T) {
	g := testGenerator()
	listing := tradertest.MustNewListing(tradertest.ListingParams{})
	proposal := tradertest.MustNewProposal(tradertest.ProposalParams{
		Listing:   listing,
		AccountID: tradertest.MustAccountID(g),
	})
	request, err := order.NewRequest(proposal, tradertest.MustOrderID(g))
	require.NoError(t, err)

	o, err := tradertest.NewOrder(tradertest.OrderParams{Request: request})
	require.NoError(t, err)

	assert.Equal(t, "broker-order-1", o.BrokerOrderID)
	assert.Equal(t, order.StatusWorking, o.Status)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(request.Quantity))
}

func TestNewOrderOverrides(t *testing.T) {
	g := testGenerator()
	listing := tradertest.MustNewListing(tradertest.ListingParams{})
	proposal := tradertest.MustNewProposal(tradertest.ProposalParams{
		Listing:   listing,
		AccountID: tradertest.MustAccountID(g),
	})
	request, err := order.NewRequest(proposal, tradertest.MustOrderID(g))
	require.NoError(t, err)

	o, err := tradertest.NewOrder(tradertest.OrderParams{
		Request:          request,
		BrokerOrderID:    "custom-broker-id",
		AcceptedQuantity: "500",
		Status:           order.StatusPartiallyFilled,
	})
	require.NoError(t, err)

	assert.Equal(t, "custom-broker-id", o.BrokerOrderID)
	assert.Equal(t, order.StatusPartiallyFilled, o.Status)
}

func TestMustNewOrderPanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		tradertest.MustNewOrder(tradertest.OrderParams{})
	})
}
