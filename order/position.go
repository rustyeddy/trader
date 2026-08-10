package order

import (
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// PositionSide is a position's holding direction: Flat, Long, or Short.
// It is deliberately a separate type from Side: an order Side is a
// transaction direction (what an order does), while PositionSide is a
// holding direction (what an account has), and Flat has no order-side
// equivalent. Flat is PositionSide's zero value, so an unconstructed
// Position is safely flat rather than silently long or short.
type PositionSide uint8

const (
	// Flat means no net exposure.
	Flat PositionSide = iota
	// Long means net exposure is held via buying.
	Long
	// Short means net exposure is held via selling.
	Short
)

// String returns a human-readable PositionSide name.
func (s PositionSide) String() string {
	switch s {
	case Flat:
		return "flat"
	case Long:
		return "long"
	case Short:
		return "short"
	default:
		return fmt.Sprintf("PositionSide(%d)", uint8(s))
	}
}

func (s PositionSide) valid() bool {
	switch s {
	case Flat, Long, Short:
		return true
	default:
		return false
	}
}

// Position is one account's net exposure in one Listing: one net
// position per account/listing pair, not a hedged or per-order
// breakdown. Quantity is always non-negative; direction lives in Side,
// matching the same convention num.Quantity itself uses.
type Position struct {
	AccountID id.AccountID
	Listing   instrument.Listing
	Side      PositionSide
	// Quantity is the position's size. It must be zero when Side is
	// Flat, and greater than zero otherwise.
	Quantity num.Quantity
	// AvgPrice is the position's volume-weighted average entry price.
	// Nil when Side is Flat.
	AvgPrice *num.Price
}

// NewPosition validates and returns a Position. AccountID must be
// non-zero and Listing must be constructed. Side must be one of its
// defined values. Quantity must be zero exactly when Side is Flat, and
// AvgPrice must be nil exactly when Side is Flat.
func NewPosition(p Position) (Position, error) {
	if p.AccountID.IsZero() {
		return Position{}, fmt.Errorf("%w: account id must be set", ErrInvalidPosition)
	}
	if p.Listing.InstrumentID().IsZero() {
		return Position{}, fmt.Errorf("%w: listing must be constructed", ErrInvalidPosition)
	}
	if !p.Side.valid() {
		return Position{}, fmt.Errorf("%w: invalid side %v", ErrInvalidPosition, p.Side)
	}
	if p.Side == Flat {
		if !p.Quantity.IsZero() {
			return Position{}, fmt.Errorf("%w: a flat position must have zero quantity", ErrInvalidPosition)
		}
		if p.AvgPrice != nil {
			return Position{}, fmt.Errorf("%w: a flat position must not have an average price", ErrInvalidPosition)
		}
	} else {
		if p.Quantity.IsZero() {
			return Position{}, fmt.Errorf("%w: a %s position must have a positive quantity", ErrInvalidPosition, p.Side)
		}
		if p.AvgPrice == nil {
			return Position{}, fmt.Errorf("%w: a %s position must have an average price", ErrInvalidPosition, p.Side)
		}
	}
	return p, nil
}
