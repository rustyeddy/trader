package trader

// openResult defines the openResult type.
type openResult struct {
	*order
	*Lot
	Fills []fill
}
