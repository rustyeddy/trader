package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/num"
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

func TestNewOrderAcceptedPriceNormalization(t *testing.T) {
	g := testGenerator()
	listing := tradertest.MustNewListing(tradertest.ListingParams{})
	proposal := tradertest.MustNewProposal(tradertest.ProposalParams{
		Listing:    listing,
		AccountID:  tradertest.MustAccountID(g),
		Type:       order.StopLimit,
		LimitPrice: "1.09000",
		StopPrice:  "1.08000",
	})
	request, err := order.NewRequest(proposal, tradertest.MustOrderID(g))
	require.NoError(t, err)

	o, err := tradertest.NewOrder(tradertest.OrderParams{
		Request:            request,
		AcceptedLimitPrice: "1.09010", // broker normalized the requested price
		AcceptedStopPrice:  "1.08010",
	})
	require.NoError(t, err)

	require.NotNil(t, o.AcceptedLimitPrice)
	require.NotNil(t, o.AcceptedStopPrice)
	assert.True(t, o.AcceptedLimitPrice.Equal(num.MustParsePrice("1.09010")))
	assert.True(t, o.AcceptedStopPrice.Equal(num.MustParsePrice("1.08010")))
	// Requested prices remain untouched, distinct from accepted ones.
	assert.True(t, request.LimitPrice.Equal(num.MustParsePrice("1.09000")))
}

func TestMustNewOrderPanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		tradertest.MustNewOrder(tradertest.OrderParams{})
	})
}
