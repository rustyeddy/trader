package trader

type openResult struct {
	*order
	*Position
	Fills []fill
}
