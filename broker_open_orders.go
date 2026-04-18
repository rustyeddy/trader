package trader

type OpenOrders struct {
	Orders map[string]*Order
}

func (o *OpenOrders) Add(od *Order) {
	o.Orders[od.ID] = od
}

func (o *OpenOrders) Get(id string) *Order {
	od, _ := o.Orders[id]
	return od
}
