package portfolio

import (
	"github.com/rustyeddy/trader/types"
)

type TradeStatus int

const (
	TradeStatusNone TradeStatus = iota
	TradeStatusOpenRequest
	TradeStatusOrder
	TradeStatusOpen
	TradeStatusCloseRequest
	TradeStatusClosed
)

type TradeCommon struct {
	ID         string
	Instrument string
	types.Side // Long or Sort
	types.Units
	Stop types.Price
	Take types.Price
}

type Trade struct {
	*TradeCommon
	ClosePrice types.Price // ExitPrice - closePrice = slippage
	ExitPrice  types.Price
	EntryTime  types.Timestamp
	ExitTime   types.Timestamp
	PNL        types.Money // account currency (best-effort)
}

func NewTrade(common *TradeCommon) *Trade {
	t := &Trade{}
	t.TradeCommon = common
	return t
}
