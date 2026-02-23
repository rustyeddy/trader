// journal/journal.go
package journal

import "github.com/rustyeddy/trader/types"

// This could go into trade or market
type TradeRecord struct {
	TradeID    string
	Instrument string
	Units      types.Units
	EntryPrice types.Price
	ExitPrice  types.Price
	OpenTime   types.Timestamp
	CloseTime  types.Timestamp
	RealizedPL types.Money
	Reason     string
}

// This could go into broker
type EquitySnapshot struct {
	Timestamp   types.Timestamp
	Balance     types.Money
	Equity      types.Money
	MarginUsed  types.Money
	FreeMargin  types.Money
	MarginLevel types.Money
}

type Journal interface {
	RecordTrade(TradeRecord) error
	RecordEquity(EquitySnapshot) error
	Close() error
}
