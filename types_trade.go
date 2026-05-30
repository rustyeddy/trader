package trader

// TradeCommon represents a trader domain type.
type TradeCommon struct {
	ID         string
	Instrument string
	Side       // Long or Short
	Units
	Stop Price
	Take Price
}

// Trade represents a trader domain type.
type Trade struct {
	*TradeCommon
	EntryPrice Price
	EntryTime  Timestamp
	ExitPrice  Price
	ExitTime   Timestamp
	PNL        Money // account currency (best-effort)
	CloseCause closeCause
}
