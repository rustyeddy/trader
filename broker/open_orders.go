package broker

import "github.com/rustyeddy/trader/types"

type OpenOrders struct {
	Orders map[string]*types.Order
}

func (o *OpenOrders) Add(od *types.Order) {
	o.Orders[od.ID] = od
}

func (o *OpenOrders) Get(id string) *types.Order {
	od, _ := o.Orders[id]
	return od
}
