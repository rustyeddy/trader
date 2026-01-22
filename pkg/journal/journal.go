// pkg/journal/journal.go
package journal

import "time"

type TradeRecord struct {
	TradeID     string
	Instrument  string
	Units       float64
	EntryPrice  float64
	ExitPrice   float64
	OpenTime    time.Time
	CloseTime   time.Time
	RealizedPL  float64
	Reason      string
}

type EquitySnapshot struct {
	Time        time.Time
	Balance     float64
	Equity      float64
	MarginUsed  float64
	FreeMargin  float64
	MarginLevel float64
}

type Journal interface {
	RecordTrade(TradeRecord) error
	RecordEquity(EquitySnapshot) error
	Close() error
}
