package execution

import "github.com/rustyeddy/trader/market"

// TradeCommon represents a trader domain type.
type TradeCommon struct {
	ID          string
	Instrument  string
	market.Side // Long or Short
	market.Units
	Stop market.Price
	Take market.Price
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
	EntryPrice market.Price
	EntryTime  market.Timestamp
	ExitPrice  market.Price
	ExitTime   market.Timestamp
	PNL        market.Money // account currency (best-effort)
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
