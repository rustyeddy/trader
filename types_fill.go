package trader

// fillStatus defines the fillStatus type.
type fillStatus uint8

const (
	FillNone fillStatus = iota
	FillComplete
	FillPartial
	FillCanceled
	FillFailed
)

// fill defines the fill type.
type fill struct {
	*TradeCommon
	fillStatus
	FillTime  Timestamp
	FillPrice Price
	FillUnits Units
}
