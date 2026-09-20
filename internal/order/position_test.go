package order

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPositionValidFlat(t *testing.T) {
	p, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      order.Flat,
	})
	require.NoError(t, err)
	assert.True(t, p.Quantity.IsZero())
	assert.Nil(t, p.AvgPrice)
}

func TestNewPositionValidLong(t *testing.T) {
	p, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      order.Long,
		Quantity:  num.MustParseQuantity("1000"),
		AvgPrice:  price(t, "1.10000"),
	})
	require.NoError(t, err)
	assert.Equal(t, order.Long, p.Side)
}

func TestNewPositionRejectsZeroAccountID(t *testing.T) {
	_, err := NewPosition(Position{Listing: mustEurUsdListing(t), Side: order.Flat})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsUnconstructedListing(t *testing.T) {
	_, err := NewPosition(Position{AccountID: mustAccountID(t), Side: order.Flat})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsInvalidSide(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      order.PositionSide(200),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsFlatWithNonZeroQuantity(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      order.Flat,
		Quantity:  num.MustParseQuantity("1"),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsFlatWithAvgPrice(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      order.Flat,
		AvgPrice:  price(t, "1.10000"),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsLongWithZeroQuantity(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      order.Long,
		AvgPrice:  price(t, "1.10000"),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsShortWithoutAvgPrice(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      order.Short,
		Quantity:  num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestPositionSideString(t *testing.T) {
	assert.Equal(t, "flat", order.Flat.String())
	assert.Equal(t, "long", order.Long.String())
	assert.Equal(t, "short", order.Short.String())
	assert.Contains(t, order.PositionSide(200).String(), "200")
}

func TestPositionSideZeroValueIsFlat(t *testing.T) {
	var s order.PositionSide
	assert.Equal(t, order.Flat, s)
}
