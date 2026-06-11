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

// Clone is an internal helper for trader type processing.
func (tc *TradeCommon) Clone() *TradeCommon {
	if tc == nil {
		return nil
	}
	cp := *tc
	return &cp
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

// Clone is an internal helper for trader type processing.
func (t *Trade) Clone() *Trade {
	if t == nil {
		return nil
	}
	cp := *t
	cp.TradeCommon = t.TradeCommon.Clone()
	return &cp
}
