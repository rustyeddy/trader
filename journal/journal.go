// journal/journal.go
package journal

import (
	"github.com/rustyeddy/trader/market"
)

// This could go into trade or market
type TradeRecord struct {
	TradeID    string
	Instrument string
	Units      market.Units
	EntryPrice market.Price
	ExitPrice  market.Price
	OpenTime   market.Timestamp
	CloseTime  market.Timestamp
	RealizedPL market.Cash
	Reason     string
}

// This could go into broker
type EquitySnapshot struct {
	Time        market.Timestamp
	Balance     market.Cash
	Equity      market.Cash
	MarginUsed  market.Cash
	FreeMargin  market.Cash
	MarginLevel market.Cash
}

type Journal interface {
	RecordTrade(TradeRecord) error
	RecordEquity(EquitySnapshot) error
	Close() error
}
