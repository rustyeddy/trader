package tradertest

import (
	"github.com/rustyeddy/trader/internal/id"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
)

// FillParams builds an order.Fill whose identifying fields (OrderID,
// BrokerOrderID, AccountID, Listing, Side) match Order, so it passes
// order.ApplyFill's identity checks. FillID is required — build one
// with MustFillID. Quantity defaults to Order's remaining quantity
// (order.Order.RemainingQuantity); Price defaults to Order's
// AcceptedLimitPrice if set, otherwise "1.10000".
type FillParams struct {
	Order        runtimeorder.Order
	FillID       id.FillID
	BrokerFillID string
	Quantity     string
	Price        string
}

// NewFillFor returns a valid order.Fill built from p, filling in
// defaults for zero-valued fields.
func NewFillFor(p FillParams) (runtimeorder.Fill, error) {
	quantity, err := fillQuantity(p)
	if err != nil {
		return runtimeorder.Fill{}, err
	}
	price, err := fillPrice(p)
	if err != nil {
		return runtimeorder.Fill{}, err
	}

	return runtimeorder.NewFill(runtimeorder.Fill{
		FillID:        p.FillID,
		OrderID:       p.Order.Request.OrderID,
		BrokerOrderID: p.Order.BrokerOrderID,
		BrokerFillID:  p.BrokerFillID,
		AccountID:     p.Order.Request.AccountID,
		Listing:       p.Order.Request.Listing,
		Side:          p.Order.Request.Side,
		Price:         price,
		Quantity:      quantity,
	})
}

func fillQuantity(p FillParams) (num.Quantity, error) {
	if p.Quantity != "" {
		return num.ParseQuantity(p.Quantity)
	}
	return p.Order.RemainingQuantity()
}

func fillPrice(p FillParams) (num.Price, error) {
	if p.Price != "" {
		return num.ParsePrice(p.Price)
	}
	if p.Order.AcceptedLimitPrice != nil {
		return *p.Order.AcceptedLimitPrice, nil
	}
	return num.ParsePrice("1.10000")
}

// MustNewFillFor is like NewFillFor but panics on error.
func MustNewFillFor(p FillParams) runtimeorder.Fill {
	f, err := NewFillFor(p)
	if err != nil {
		panic(err)
	}
	return f
}
