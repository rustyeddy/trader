package types

type OpenResult struct {
	*order.Order
	Fills []order.Fill
}
