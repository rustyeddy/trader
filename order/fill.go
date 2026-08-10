package order

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// Fill is one execution against an Order. It preserves both Trader's own
// FillID and the broker's native execution identifier, BrokerFillID,
// alongside the parent order's identifiers — a broker execution normally
// has its own native identity independent of its parent order, which
// matters for reconciliation and deduplicating broker execution events.
type Fill struct {
	// FillID is Trader's own identifier for this fill.
	FillID id.FillID
	// OrderID is the Trader OrderID of the order this fill executed
	// against.
	OrderID id.OrderID
	// BrokerOrderID is the broker's identifier for the parent order.
	BrokerOrderID string
	// BrokerFillID is the broker's own identifier for this specific
	// execution, preserved without replacing FillID.
	BrokerFillID string
	// AccountID identifies the account this execution belongs to. It is
	// carried directly rather than requiring a join through the parent
	// Order, since a fill is a broker/account event in its own right.
	AccountID id.AccountID
	// Listing is the venue-specific tradable instrument that executed.
	Listing instrument.Listing
	Side    Side
	// Price is this execution's price.
	Price num.Price
	// Quantity is this execution's size. It must be greater than zero.
	Quantity num.Quantity
	// Commission is this execution's fee, if reported. Nil if not
	// reported.
	Commission *num.Money
	// Timestamp is when the execution occurred, as reported by the
	// broker.
	Timestamp time.Time
	// Metadata carries this fill's correlation and causation context,
	// typically with CausationID pointing at the parent order's
	// EventID.
	Metadata id.Metadata
}

// NewFill validates and returns a Fill. FillID, OrderID, and AccountID
// must be non-zero; Listing must be constructed; Side must be one of its
// defined values; Quantity must be positive and a multiple of Listing's
// quantity increment; Price must be an exact multiple of Listing's tick
// size.
func NewFill(f Fill) (Fill, error) {
	if f.FillID.IsZero() {
		return Fill{}, fmt.Errorf("%w: fill id must be set", ErrInvalidFill)
	}
	if f.OrderID.IsZero() {
		return Fill{}, fmt.Errorf("%w: order id must be set", ErrInvalidFill)
	}
	if f.AccountID.IsZero() {
		return Fill{}, fmt.Errorf("%w: account id must be set", ErrInvalidFill)
	}
	if f.Listing.InstrumentID().IsZero() {
		return Fill{}, fmt.Errorf("%w: listing must be constructed", ErrInvalidFill)
	}
	if !f.Side.valid() {
		return Fill{}, fmt.Errorf("%w: invalid side %v", ErrInvalidFill, f.Side)
	}
	if f.Quantity.IsZero() {
		return Fill{}, fmt.Errorf("%w: quantity must be greater than zero", ErrInvalidFill)
	}
	if err := f.Listing.Spec().ValidateQuantity(f.Quantity); err != nil {
		return Fill{}, fmt.Errorf("%w: %v", ErrInvalidFill, err)
	}
	if err := f.Listing.Spec().ValidatePrice(f.Price); err != nil {
		return Fill{}, fmt.Errorf("%w: %v", ErrInvalidFill, err)
	}
	return f, nil
}
