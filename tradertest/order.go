package tradertest

import (
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// OrderParams builds an accepted order.Order from an existing
// order.Request. Request is required; build one with
// order.NewRequest(proposal, orderID) — tradertest does not duplicate
// that constructor, since it already takes exactly the two things a
// caller has at that point and nothing more.
//
// BrokerOrderID and Status default to "broker-order-1" and
// order.StatusWorking. AcceptedQuantity, AcceptedLimitPrice, and
// AcceptedStopPrice default to Request's own Quantity, LimitPrice, and
// StopPrice — the common case of a broker accepting a request exactly
// as submitted; set AcceptedQuantity (and, if relevant,
// AcceptedLimitPrice/AcceptedStopPrice) to test broker-side
// normalization.
//
// NewOrder always builds an accepted order, so Status must be one that
// requires acceptance (order.StatusWorking and beyond). For an
// unaccepted order (StatusPendingSubmit or StatusRejected), construct
// order.Order directly — there is no repeated boilerplate here to
// replace, since that shape appears once in M1's own tests.
type OrderParams struct {
	Request       order.Request
	BrokerOrderID string
	// AcceptedQuantity is decimal text. Empty means "use
	// Request.Quantity".
	AcceptedQuantity string
	// AcceptedLimitPrice and AcceptedStopPrice are decimal text. Empty
	// means "use Request.LimitPrice"/"use Request.StopPrice". Set
	// either to test broker-side price normalization specifically;
	// most tests only need to override AcceptedQuantity.
	AcceptedLimitPrice string
	AcceptedStopPrice  string
	Status             order.Status
}

// NewOrder returns a valid, accepted order.Order built from p, filling
// in defaults for zero-valued fields.
func NewOrder(p OrderParams) (order.Order, error) {
	if p.BrokerOrderID == "" {
		p.BrokerOrderID = "broker-order-1"
	}
	if p.Status == 0 {
		p.Status = order.StatusWorking
	}

	acceptedQuantity := p.Request.Quantity
	if p.AcceptedQuantity != "" {
		q, err := num.ParseQuantity(p.AcceptedQuantity)
		if err != nil {
			return order.Order{}, err
		}
		acceptedQuantity = q
	}

	acceptedLimitPrice := p.Request.LimitPrice
	if p.AcceptedLimitPrice != "" {
		lp, err := num.ParsePrice(p.AcceptedLimitPrice)
		if err != nil {
			return order.Order{}, err
		}
		acceptedLimitPrice = &lp
	}

	acceptedStopPrice := p.Request.StopPrice
	if p.AcceptedStopPrice != "" {
		sp, err := num.ParsePrice(p.AcceptedStopPrice)
		if err != nil {
			return order.Order{}, err
		}
		acceptedStopPrice = &sp
	}

	return order.NewOrder(order.Order{
		Request:            p.Request,
		BrokerOrderID:      p.BrokerOrderID,
		AcceptedQuantity:   &acceptedQuantity,
		AcceptedLimitPrice: acceptedLimitPrice,
		AcceptedStopPrice:  acceptedStopPrice,
		Status:             p.Status,
	})
}

// MustNewOrder is like NewOrder but panics on error.
func MustNewOrder(p OrderParams) order.Order {
	o, err := NewOrder(p)
	if err != nil {
		panic(err)
	}
	return o
}
