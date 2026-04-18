package trader

type OpenResult struct {
	*Order
	*Position
	Fills []Fill
}
