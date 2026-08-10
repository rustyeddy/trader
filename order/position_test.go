package order

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPositionValidFlat(t *testing.T) {
	p, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Flat,
	})
	require.NoError(t, err)
	assert.True(t, p.Quantity.IsZero())
	assert.Nil(t, p.AvgPrice)
}

func TestNewPositionValidLong(t *testing.T) {
	p, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Long,
		Quantity:  num.MustParseQuantity("1000"),
		AvgPrice:  price(t, "1.10000"),
	})
	require.NoError(t, err)
	assert.Equal(t, Long, p.Side)
}

func TestNewPositionRejectsZeroAccountID(t *testing.T) {
	_, err := NewPosition(Position{Listing: mustEurUsdListing(t), Side: Flat})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsUnconstructedListing(t *testing.T) {
	_, err := NewPosition(Position{AccountID: mustAccountID(t), Side: Flat})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsInvalidSide(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      PositionSide(200),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsFlatWithNonZeroQuantity(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Flat,
		Quantity:  num.MustParseQuantity("1"),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsFlatWithAvgPrice(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Flat,
		AvgPrice:  price(t, "1.10000"),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsLongWithZeroQuantity(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Long,
		AvgPrice:  price(t, "1.10000"),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestNewPositionRejectsShortWithoutAvgPrice(t *testing.T) {
	_, err := NewPosition(Position{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Short,
		Quantity:  num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidPosition)
}

func TestPositionSideString(t *testing.T) {
	assert.Equal(t, "flat", Flat.String())
	assert.Equal(t, "long", Long.String())
	assert.Equal(t, "short", Short.String())
	assert.Contains(t, PositionSide(200).String(), "200")
}

func TestPositionSideZeroValueIsFlat(t *testing.T) {
	var s PositionSide
	assert.Equal(t, Flat, s)
}
