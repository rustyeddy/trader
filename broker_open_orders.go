package trader

type OpenOrders struct {
	Orders map[string]*order
}

func (o *OpenOrders) Add(od *order) {
	o.Orders[od.ID] = od
}

func (o *OpenOrders) Get(id string) *order {
	od, _ := o.Orders[id]
	return od
}
