package trader

// TradeRecord is the canonical persisted representation of a completed trade.
// It is shared by live journaling, replay/sim journaling, and export formats
// such as CSV, JSONL, and Org output.
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

// EquitySnapshot captures account state at a point in time for journal backends
// that persist balance/equity history alongside completed trades.
type EquitySnapshot struct {
	Timestamp   Timestamp
	Balance     Money
	Equity      Money
	MarginUsed  Money
	FreeMargin  Money
	MarginLevel Money
}

// Journal is the storage contract used by live trading and replay code to
// persist completed trades and optional equity snapshots.
type Journal interface {
	RecordTrade(TradeRecord) error
	RecordEquity(EquitySnapshot) error
	Close() error
}
