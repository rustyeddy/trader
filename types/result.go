package types

type OpenResult struct {
	*Order
	*Position
	Fills []Fill
}
