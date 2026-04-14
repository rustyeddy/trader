package types

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
	Side       // Long or Sort
	Units
	Stop Price
	Take Price
}

type Trade struct {
	*TradeCommon
	ClosePrice Price // ExitPrice - closePrice = slippage
	ExitPrice  Price
	EntryTime  Timestamp
	ExitTime   Timestamp
	PNL        Money // account currency (best-effort)
}

func NewTrade(common *TradeCommon) *Trade {
	t := &Trade{}
	t.TradeCommon = common
	return t
}
