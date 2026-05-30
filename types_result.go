package trader

// openResult represents a trader domain type.
type openResult struct {
	*order
	*Lot
	Fills []fill
}
