package order

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/num"
)

// Order is a broker-recognized order and its lifecycle state. It embeds
// the originating Request — preserving the requested identity and
// values Trader submitted — alongside the broker's separately tracked
// accepted/effective values, since a broker may accept a request but
// normalize it (tick rounding, quantity increment, venue rules).
// Accepted* fields are nil until the broker has accepted the order.
type Order struct {
	// Request is the request that created this order, preserving its
	// requested quantity and prices for comparison and audit.
	Request Request
	// BrokerOrderID is the broker's own identifier for this order,
	// preserved without replacing OrderID. It is empty until the
	// broker acknowledges the order.
	BrokerOrderID string

	// AcceptedQuantity is the quantity the broker actually accepted,
	// which may differ from Request.Quantity. Nil until accepted.
	AcceptedQuantity *num.Quantity
	// AcceptedLimitPrice is the limit price the broker actually
	// accepted. Present or nil exactly as Request.Type requires, once
	// AcceptedQuantity is set.
	AcceptedLimitPrice *num.Price
	// AcceptedStopPrice is the stop price the broker actually accepted.
	// Present or nil exactly as Request.Type requires, once
	// AcceptedQuantity is set.
	AcceptedStopPrice *num.Price

	Status Status
	// FilledQuantity is the cumulative quantity executed so far. It can
	// only be non-zero once AcceptedQuantity is set, and never exceeds
	// it.
	FilledQuantity num.Quantity
	// AvgFillPrice is the quantity-weighted average price across all
	// fills so far. Nil until the first fill.
	AvgFillPrice *num.Price
	// Rejection explains why the broker declined this order. Non-nil
	// exactly when Status is StatusRejected.
	Rejection *Rejection

	// UpdatedAt is when this Order value was last observed to change.
	UpdatedAt time.Time
}

// NewOrder validates and returns an Order. o.Request is fully
// revalidated, including its embedded Proposal — not merely trusted by
// provenance, since Request's exported fields let a caller build one as
// a bare struct literal. o.Status must be one of its defined values. If
// o.AcceptedQuantity is non-nil, it must be positive, a multiple of the
// listing's quantity increment, and AcceptedLimitPrice/AcceptedStopPrice
// must be present or nil exactly as o.Request.Type requires; if non-nil,
// they must be exact multiples of the listing's tick size.
// o.FilledQuantity must not exceed AcceptedQuantity, and must be zero if
// AcceptedQuantity is nil. A Status that implies the broker has accepted
// the order (StatusWorking, StatusPartiallyFilled, StatusFilled,
// StatusPendingCancel, StatusCanceled, StatusPendingReplace,
// StatusExpired) requires a non-nil AcceptedQuantity; a Status that
// implies it has not (StatusPendingSubmit, StatusRejected) requires a
// nil one. o.Rejection must be non-nil exactly when o.Status is
// StatusRejected.
//
// NewOrder validates this snapshot's internal consistency only. Whether
// a given Status is reachable from the order's prior state is issue #29
// (M1-11)'s responsibility, not this constructor's.
func NewOrder(o Order) (Order, error) {
	if err := checkRequest(o.Request); err != nil {
		return Order{}, fmt.Errorf("%w: request: %v", ErrInvalidOrder, err)
	}
	if !o.Status.valid() {
		return Order{}, fmt.Errorf("%w: invalid status %v", ErrInvalidOrder, o.Status)
	}

	if o.AcceptedQuantity != nil {
		if o.AcceptedQuantity.IsZero() {
			return Order{}, fmt.Errorf("%w: accepted quantity must be greater than zero", ErrInvalidOrder)
		}
		if err := validatePricePresence(o.Request.Type, o.AcceptedLimitPrice, o.AcceptedStopPrice); err != nil {
			return Order{}, fmt.Errorf("%w: accepted %v", ErrInvalidOrder, err)
		}
		if err := validatePriceAndQuantity(o.Request.Listing, *o.AcceptedQuantity, o.AcceptedLimitPrice, o.AcceptedStopPrice); err != nil {
			return Order{}, fmt.Errorf("%w: accepted %v", ErrInvalidOrder, err)
		}
		if _, err := o.AcceptedQuantity.Sub(o.FilledQuantity); err != nil {
			return Order{}, fmt.Errorf("%w: filled quantity exceeds accepted quantity: %v", ErrInvalidOrder, err)
		}
	} else if !o.FilledQuantity.IsZero() {
		return Order{}, fmt.Errorf("%w: filled quantity requires an accepted quantity", ErrInvalidOrder)
	} else if o.AcceptedLimitPrice != nil || o.AcceptedStopPrice != nil {
		return Order{}, fmt.Errorf("%w: accepted prices require an accepted quantity", ErrInvalidOrder)
	}

	if o.Status.requiresAcceptance() && o.AcceptedQuantity == nil {
		return Order{}, fmt.Errorf("%w: status %s requires an accepted quantity", ErrInvalidOrder, o.Status)
	}
	if o.Status.precludesAcceptance() && o.AcceptedQuantity != nil {
		return Order{}, fmt.Errorf("%w: status %s must not have an accepted quantity", ErrInvalidOrder, o.Status)
	}

	if o.Status == StatusRejected && o.Rejection == nil {
		return Order{}, fmt.Errorf("%w: rejected order requires a Rejection", ErrInvalidOrder)
	}
	if o.Status != StatusRejected && o.Rejection != nil {
		return Order{}, fmt.Errorf("%w: only a rejected order may carry a Rejection", ErrInvalidOrder)
	}

	return o, nil
}

// RemainingQuantity returns AcceptedQuantity minus FilledQuantity. It
// reports ErrInvalidOrder if the order has not yet been accepted, so
// remaining quantity is never confused with the originally requested
// quantity.
func (o Order) RemainingQuantity() (num.Quantity, error) {
	if o.AcceptedQuantity == nil {
		return num.Quantity{}, fmt.Errorf("%w: order has not been accepted", ErrInvalidOrder)
	}
	return o.AcceptedQuantity.Sub(o.FilledQuantity)
}
