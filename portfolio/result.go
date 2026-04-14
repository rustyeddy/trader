package portfolio

type OpenResult struct {
	*order.Order
	Fills []order.Fill
}
