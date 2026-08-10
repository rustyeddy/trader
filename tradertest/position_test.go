package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPositionDefaults(t *testing.T) {
	g := testGenerator()
	p, err := tradertest.NewPosition(tradertest.PositionParams{
		AccountID: tradertest.MustAccountID(g),
		Listing:   tradertest.MustNewListing(tradertest.ListingParams{}),
	})
	require.NoError(t, err)

	assert.Equal(t, order.Long, p.Side)
	require.NotNil(t, p.AvgPrice)
}

func TestNewPositionShortOverride(t *testing.T) {
	g := testGenerator()
	p, err := tradertest.NewPosition(tradertest.PositionParams{
		AccountID: tradertest.MustAccountID(g),
		Listing:   tradertest.MustNewListing(tradertest.ListingParams{}),
		Side:      order.Short,
		Quantity:  "250",
		AvgPrice:  "1.25000",
	})
	require.NoError(t, err)

	assert.Equal(t, order.Short, p.Side)
}

func TestNewPositionRejectsZeroAccountID(t *testing.T) {
	_, err := tradertest.NewPosition(tradertest.PositionParams{
		Listing: tradertest.MustNewListing(tradertest.ListingParams{}),
	})
	require.Error(t, err)
}

func TestMustNewPositionPanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		tradertest.MustNewPosition(tradertest.PositionParams{})
	})
}
