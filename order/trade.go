package order

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// Trade is a derived reporting concept grouping the entry and exit fills
// of one round-trip position, distinct from a broker's own notion of an
// order or execution. Trade deliberately has no first-class identity in
// this package: it can be recalculated, revised, or represented across
// multiple journal events as fills continue to arrive, unlike an EventID
// or FillID, which identifies one immutable event. A dedicated TradeID
// is additive future work if M5's journal/report work later finds that
// insufficient — not introduced speculatively here.
type Trade struct {
	AccountID id.AccountID
	Listing   instrument.Listing
	// Side is the trade's holding direction while it was open.
	Side PositionSide
	// EntryFillIDs are the fills that opened or added to the position.
	EntryFillIDs []id.FillID
	// ExitFillIDs are the fills that reduced or closed the position.
	ExitFillIDs []id.FillID
	// OpenedAt is the timestamp of the first entry fill.
	OpenedAt time.Time
	// ClosedAt is the timestamp of the last exit fill. It is the zero
	// time for a trade that has not fully closed.
	ClosedAt time.Time
	// RealizedPnL is this trade's realized profit or loss so far.
	RealizedPnL num.Money
}

// NewTrade validates and returns a Trade. AccountID must be non-zero,
// Listing must be constructed, Side must be Long or Short (never Flat —
// a trade with no direction has nothing to report), at least one entry
// fill must be present, and OpenedAt must be non-zero.
func NewTrade(t Trade) (Trade, error) {
	if t.AccountID.IsZero() {
		return Trade{}, fmt.Errorf("%w: account id must be set", ErrInvalidTrade)
	}
	if t.Listing.InstrumentID().IsZero() {
		return Trade{}, fmt.Errorf("%w: listing must be constructed", ErrInvalidTrade)
	}
	if t.Side != Long && t.Side != Short {
		return Trade{}, fmt.Errorf("%w: side must be long or short, got %v", ErrInvalidTrade, t.Side)
	}
	if len(t.EntryFillIDs) == 0 {
		return Trade{}, fmt.Errorf("%w: at least one entry fill is required", ErrInvalidTrade)
	}
	for _, fillID := range t.EntryFillIDs {
		if fillID.IsZero() {
			return Trade{}, fmt.Errorf("%w: entry fill ids must be non-zero", ErrInvalidTrade)
		}
	}
	for _, fillID := range t.ExitFillIDs {
		if fillID.IsZero() {
			return Trade{}, fmt.Errorf("%w: exit fill ids must be non-zero", ErrInvalidTrade)
		}
	}
	if t.OpenedAt.IsZero() {
		return Trade{}, fmt.Errorf("%w: opened at must be set", ErrInvalidTrade)
	}
	return t, nil
}
