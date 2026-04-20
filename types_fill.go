package trader

type fillStatus uint8

const (
	FillNone fillStatus = iota
	FillComplete
	FillPartial
	FillCanceled
	FillFailed
)

type fill struct {
	*TradeCommon
	fillStatus
	FillTime  Timestamp
	FillPrice Price
	FillUnits Units
}
