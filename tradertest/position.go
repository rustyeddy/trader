package tradertest

import (
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// PositionParams builds a non-flat order.Position. Side, Quantity, and
// AvgPrice default to order.Long, "1000", and "1.10000". AccountID and
// Listing are required.
//
// order.Flat is Side's zero value, so NewPosition cannot distinguish
// "unset, use the default" from "explicitly Flat" — and a Flat position
// needs a zero Quantity and nil AvgPrice, which this builder's own
// defaults don't produce. Build a Flat order.Position directly with
// order.NewPosition; there is no repeated boilerplate for that case to
// replace.
type PositionParams struct {
	AccountID id.AccountID
	Listing   instrument.Listing
	Side      order.PositionSide
	Quantity  string
	AvgPrice  string
}

// NewPosition returns a valid order.Position built from p, filling in
// defaults for zero-valued fields.
func NewPosition(p PositionParams) (order.Position, error) {
	if p.Side == order.Flat {
		p.Side = order.Long
	}
	if p.Quantity == "" {
		p.Quantity = "1000"
	}
	if p.AvgPrice == "" {
		p.AvgPrice = "1.10000"
	}

	quantity, err := num.ParseQuantity(p.Quantity)
	if err != nil {
		return order.Position{}, err
	}
	avgPrice, err := num.ParsePrice(p.AvgPrice)
	if err != nil {
		return order.Position{}, err
	}

	return order.NewPosition(order.Position{
		AccountID: p.AccountID,
		Listing:   p.Listing,
		Side:      p.Side,
		Quantity:  quantity,
		AvgPrice:  &avgPrice,
	})
}

// MustNewPosition is like NewPosition but panics on error.
func MustNewPosition(p PositionParams) order.Position {
	pos, err := NewPosition(p)
	if err != nil {
		panic(err)
	}
	return pos
}
