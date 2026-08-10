package order

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFillValid(t *testing.T) {
	f, err := NewFill(Fill{
		FillID:        mustFillID(t),
		OrderID:       mustOrderID(t),
		BrokerOrderID: "broker-order-1",
		BrokerFillID:  "broker-fill-1",
		AccountID:     mustAccountID(t),
		Listing:       mustEurUsdListing(t),
		Side:          Buy,
		Price:         num.MustParsePrice("1.10000"),
		Quantity:      num.MustParseQuantity("1000"),
		Timestamp:     time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, "broker-fill-1", f.BrokerFillID)
}

func TestNewFillRejectsZeroFillID(t *testing.T) {
	_, err := NewFill(Fill{
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Buy,
		Price:     num.MustParsePrice("1.10000"),
		Quantity:  num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidFill)
}

func TestNewFillRejectsZeroOrderID(t *testing.T) {
	_, err := NewFill(Fill{
		FillID:    mustFillID(t),
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Buy,
		Price:     num.MustParsePrice("1.10000"),
		Quantity:  num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidFill)
}

func TestNewFillRejectsZeroAccountID(t *testing.T) {
	_, err := NewFill(Fill{
		FillID:   mustFillID(t),
		OrderID:  mustOrderID(t),
		Listing:  mustEurUsdListing(t),
		Side:     Buy,
		Price:    num.MustParsePrice("1.10000"),
		Quantity: num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidFill)
}

func TestNewFillRejectsUnconstructedListing(t *testing.T) {
	_, err := NewFill(Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Side:      Buy,
		Price:     num.MustParsePrice("1.10000"),
		Quantity:  num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidFill)
}

func TestNewFillRejectsInvalidSide(t *testing.T) {
	_, err := NewFill(Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Price:     num.MustParsePrice("1.10000"),
		Quantity:  num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidFill)
}

func TestNewFillRejectsZeroQuantity(t *testing.T) {
	_, err := NewFill(Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Buy,
		Price:     num.MustParsePrice("1.10000"),
	})
	assert.ErrorIs(t, err, ErrInvalidFill)
}

func TestNewFillRejectsPriceNotOnTick(t *testing.T) {
	_, err := NewFill(Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Buy,
		Price:     num.MustParsePrice("1.10000123"),
		Quantity:  num.MustParseQuantity("1000"),
	})
	assert.ErrorIs(t, err, ErrInvalidFill)
}

func TestNewFillRejectsQuantityNotOnIncrement(t *testing.T) {
	_, err := NewFill(Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Buy,
		Price:     num.MustParsePrice("1.10000"),
		Quantity:  num.MustParseQuantity("1000.5"),
	})
	assert.ErrorIs(t, err, ErrInvalidFill)
}

func TestFillPreservesBothBrokerAndTraderIdentifiers(t *testing.T) {
	f, err := NewFill(Fill{
		FillID:        mustFillID(t),
		OrderID:       mustOrderID(t),
		BrokerOrderID: "bo-1",
		BrokerFillID:  "bf-1",
		AccountID:     mustAccountID(t),
		Listing:       mustEurUsdListing(t),
		Side:          Sell,
		Price:         num.MustParsePrice("1.10000"),
		Quantity:      num.MustParseQuantity("1000"),
		Metadata:      id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)
	assert.False(t, f.FillID.IsZero())
	assert.False(t, f.OrderID.IsZero())
	assert.Equal(t, "bo-1", f.BrokerOrderID)
	assert.Equal(t, "bf-1", f.BrokerFillID)
}
