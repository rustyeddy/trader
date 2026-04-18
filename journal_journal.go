// journal/journal.go
package trader

// This could go into trade or market
type TradeRecord struct {
	TradeID    string
	Instrument string
	Units      Units
	EntryPrice Price
	ExitPrice  Price
	OpenTime   Timestamp
	CloseTime  Timestamp
	RealizedPL Money
	Reason     string
}

// This could go into broker
type EquitySnapshot struct {
	Timestamp   Timestamp
	Balance     Money
	Equity      Money
	MarginUsed  Money
	FreeMargin  Money
	MarginLevel Money
}

type Journal interface {
	RecordTrade(TradeRecord) error
	RecordEquity(EquitySnapshot) error
	Close() error
}
