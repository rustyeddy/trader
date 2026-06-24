package journal

import "github.com/rustyeddy/trader/market"

// TradeRecord is the canonical persisted representation of a completed trade.
// It is shared by live journaling, replay/sim journaling, and export formats
// such as CSV, JSONL, and Org output.
type TradeRecord struct {
	TradeID    string
	BotID      string // set by the bot manager; empty for backtest/journal-only runs
	Instrument string
	Units      market.Units
	EntryPrice market.Price
	ExitPrice  market.Price
	OpenTime   market.Timestamp
	CloseTime  market.Timestamp
	RealizedPL market.Money
	Reason     string
}

// EquitySnapshot captures account state at a point in time for journal backends
// that persist balance/equity history alongside completed trades.
type EquitySnapshot struct {
	Timestamp   market.Timestamp
	Balance     market.Money
	Equity      market.Money
	MarginUsed  market.Money
	FreeMargin  market.Money
	MarginLevel market.Money
}

// Journal is the storage contract used by live trading and replay code to
// persist completed trades and optional equity snapshots.
type Journal interface {
	RecordTrade(TradeRecord) error
	RecordEquity(EquitySnapshot) error
	Close() error
}
