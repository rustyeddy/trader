package types

type FillStatus uint8

const (
	FillNone FillStatus = iota
	FillComplete
	FillPartial
	FillCanceled
	FillFailed
)

type Fill struct {
	*TradeCommon
	FillStatus
	FillTime  Timestamp
	FillPrice Price
	FillUnits Units
}
