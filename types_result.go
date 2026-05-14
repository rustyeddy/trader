package trader

type openResult struct {
	*order
	*Lot
	Fills []fill
}
