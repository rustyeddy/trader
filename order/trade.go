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
	// RealizedPnL is this trade's realized profit or loss so far,
	// excluding Costs — a trade's net result is RealizedPnL minus
	// Costs, kept as two values rather than one combined figure so a
	// caller can report gross P&L and cost drag separately.
	RealizedPnL num.Money
	// Costs is the cumulative commission attributed to this trade so
	// far, summed from every entry/exit fill's own order.Fill.Commission
	// (issue #217, M5-09). A fill that both closes this trade and opens
	// a new one in the opposite direction (a reversal) has its
	// commission split pro-rata by quantity between the two trades; see
	// the trade-deriving caller for the exact split rule.
	Costs num.Money
}

// NewTrade validates and returns a Trade. AccountID must be non-zero,
// Listing must be constructed, Side must be Long or Short (never Flat —
// a trade with no direction has nothing to report), at least one entry
// fill must be present, and OpenedAt must be non-zero. If ClosedAt is
// set, it must not precede OpenedAt; ClosedAt is not otherwise required
// merely because ExitFillIDs is non-empty, since partial exits are
// legitimate for a still-open trade. RealizedPnL and Costs must both be
// valid Money (num.Money.IsValid) — an explicit, currency-denominated
// zero, never the bare Go zero value — the same discipline
// account.Snapshot's own Money fields already require.
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
	if !t.ClosedAt.IsZero() && t.ClosedAt.Before(t.OpenedAt) {
		return Trade{}, fmt.Errorf("%w: closed at must not precede opened at", ErrInvalidTrade)
	}
	if !t.RealizedPnL.IsValid() {
		return Trade{}, fmt.Errorf("%w: realized pnl must be valid money", ErrInvalidTrade)
	}
	if !t.Costs.IsValid() {
		return Trade{}, fmt.Errorf("%w: costs must be valid money", ErrInvalidTrade)
	}
	if !t.RealizedPnL.Currency().Equal(t.Costs.Currency()) {
		return Trade{}, fmt.Errorf("%w: realized pnl currency %s does not match costs currency %s", ErrInvalidTrade, t.RealizedPnL.Currency(), t.Costs.Currency())
	}
	return t, nil
}
