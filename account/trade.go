package account

import "github.com/rustyeddy/trader/types"

// TradeCommon represents a trader domain type.
type TradeCommon struct {
	ID         string
	Instrument string
	types.Side // Long or Short
	types.Units
	Stop types.Price
	Take types.Price

	// Reason is the entry signal's Signal.Reason, captured once when the
	// lot opens. Unlike Stop (overwritten every bar by trailing/chandelier
	// exits), Reason and InitialStop are never mutated after open, so they
	// survive intact through Lot -> Trade on close. Analysis tooling (e.g.
	// signalreplay) uses Reason to join a closed trade back to the signal
	// that opened it.
	Reason string
	// InitialStop is the stop price actually used to open the lot (after
	// the exit strategy's InitialStop and the DefaultStopPips fallback are
	// resolved), captured once at open. See Reason for why this needs its
	// own field instead of reading Stop after the fact.
	InitialStop types.Price
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
