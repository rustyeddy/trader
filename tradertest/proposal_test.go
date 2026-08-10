package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProposalDefaults(t *testing.T) {
	g := testGenerator()
	p, err := tradertest.NewProposal(tradertest.ProposalParams{
		Listing:   tradertest.MustNewListing(tradertest.ListingParams{}),
		AccountID: tradertest.MustAccountID(g),
	})
	require.NoError(t, err)

	assert.Equal(t, order.Buy, p.Side)
	assert.Equal(t, order.Market, p.Type)
	assert.Equal(t, order.GTC, p.TimeInForce)
	assert.True(t, p.Quantity.Equal(num.MustParseQuantity("1000")))
	assert.Nil(t, p.LimitPrice)
	assert.Nil(t, p.StopPrice)
}

func TestNewProposalLimitOrderRequiresLimitPrice(t *testing.T) {
	g := testGenerator()
	_, err := tradertest.NewProposal(tradertest.ProposalParams{
		Listing:   tradertest.MustNewListing(tradertest.ListingParams{}),
		AccountID: tradertest.MustAccountID(g),
		Type:      order.Limit,
	})
	require.Error(t, err)
}

func TestNewProposalLimitOrderWithPrice(t *testing.T) {
	g := testGenerator()
	p, err := tradertest.NewProposal(tradertest.ProposalParams{
		Listing:    tradertest.MustNewListing(tradertest.ListingParams{}),
		AccountID:  tradertest.MustAccountID(g),
		Type:       order.Limit,
		LimitPrice: "1.09000",
	})
	require.NoError(t, err)
	require.NotNil(t, p.LimitPrice)
	assert.True(t, p.LimitPrice.Equal(num.MustParsePrice("1.09000")))
}

func TestNewProposalExplicitSideAndTimeInForce(t *testing.T) {
	g := testGenerator()
	p, err := tradertest.NewProposal(tradertest.ProposalParams{
		Listing:     tradertest.MustNewListing(tradertest.ListingParams{}),
		AccountID:   tradertest.MustAccountID(g),
		Side:        order.Sell,
		Type:        order.Stop,
		TimeInForce: order.GTC,
		Quantity:    "250",
		StopPrice:   "1.05000",
		ReduceOnly:  true,
	})
	require.NoError(t, err)

	assert.Equal(t, order.Sell, p.Side)
	assert.Equal(t, order.Stop, p.Type)
	assert.True(t, p.ReduceOnly)
	require.NotNil(t, p.StopPrice)
}

func TestNewProposalRejectsZeroAccountID(t *testing.T) {
	_, err := tradertest.NewProposal(tradertest.ProposalParams{
		Listing: tradertest.MustNewListing(tradertest.ListingParams{}),
	})
	require.Error(t, err)
}

func TestMustNewProposalPanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		tradertest.MustNewProposal(tradertest.ProposalParams{})
	})
}
