package strategy

import (
	"context"

	runtimeorder "github.com/rustyeddy/trader/internal/order"
)

// FillEvent is one order.Fill delivered to a strategy implementing
// FillHandler. order.Fill already carries OrderID, Listing, Side,
// Price, Quantity, and Metadata (CorrelationID/CausationID), which is
// enough identity/causality for a strategy to tell what filled — an
// instrument it does not own (Listing.InstrumentID()), an entry versus
// a protective exit for a long-only strategy (Side), or a specific
// bracket attempt (Metadata.CorrelationID, propagated unchanged from
// the originating order.Intent through Proposal/Decision/Request/Order
// per ADR-005's own correlation convention) — so FillEvent wraps it
// directly rather than duplicating or renaming any of its fields
// (ADR-060).
type FillEvent struct {
	Fill runtimeorder.Fill
}

// FillHandler is an optional Strategy capability (ADR-060): a strategy
// implementing it receives every order.Fill for its own account, in
// delivery order, before the OnBar call for whichever bar the fill is
// attributed to. Most strategies do not need this — View.Account()'s
// own Positions() snapshot is normally enough — so this stays an
// optional capability, matching History's own "small required core
// plus capability discovery" pattern rather than widening the
// required Strategy interface.
//
// OnFill returns only error, deliberately not []order.Intent: emitting
// intents directly from a fill callback needs its own design (ordering
// and recursion within a batch, next-bar-open eligibility, warm-up
// interaction, and eventual live/paper semantics all matter) that no
// current strategy needs yet. See ADR-060's own Alternatives
// Considered for why this was decided now rather than deferred as a
// TODO on a wider signature.
type FillHandler interface {
	OnFill(ctx context.Context, event FillEvent, view View) error
}
