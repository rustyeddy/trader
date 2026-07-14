package execution

import "github.com/rustyeddy/trader/types"

// TradeCommon represents a trader domain type.
type TradeCommon struct {
	ID         string
	Instrument string
	types.Side // Long or Short
	types.Units
	Stop types.Price
	Take types.Price
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
	EntryPrice types.Price
	EntryTime  types.Timestamp
	ExitPrice  types.Price
	ExitTime   types.Timestamp
	PNL        types.Money // account currency (best-effort)
	CloseCause CloseCause
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
