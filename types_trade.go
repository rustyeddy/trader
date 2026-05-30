package trader

// TradeCommon defines the TradeCommon type.
type TradeCommon struct {
	ID         string
	Instrument string
	Side       // Long or Short
	Units
	Stop Price
	Take Price
}

// Trade defines the Trade type.
type Trade struct {
	*TradeCommon
	EntryPrice Price
	EntryTime  Timestamp
	ExitPrice  Price
	ExitTime   Timestamp
	PNL        Money // account currency (best-effort)
	CloseCause closeCause
}
