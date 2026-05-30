package trader

// fillStatus represents a trader domain type.
type fillStatus uint8

const (
	FillNone fillStatus = iota
	FillComplete
	FillPartial
	FillCanceled
	FillFailed
)

// fill represents a trader domain type.
type fill struct {
	*TradeCommon
	fillStatus
	FillTime  Timestamp
	FillPrice Price
	FillUnits Units
}
