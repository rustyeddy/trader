package portfolio

import "github.com/rustyeddy/trader/types"

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
	FillTime  types.Timestamp
	FillPrice types.Price
	FillUnits types.Units
}
