package broker

import "github.com/rustyeddy/trader/order"

type OpenOrders struct {
	Orders map[string]*order.Order
}

func (o *OpenOrders) Add(od *order.Order) {
	o.Orders[od.ID] = od
}

func (o *OpenOrders) Get(id string) *order.Order {
	od, _ := o.Orders[id]
	return od
}
