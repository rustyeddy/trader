package sdk

import (
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// DescribedIntent is a guest's own requested trading action —
// sdk's own counterpart to order.Intent, but never a real one
// (see the package doc comment's own "intent construction ownership"
// section): it carries no IntentID/EventID/CorrelationID, only Kind,
// instrument, and whichever of Side/Quantity/StopPrice that Kind
// requires — the exact shape strategy.proto's own DescribedIntent
// message carries. Build one with Enter, Exit, AdjustStop,
// EnterWithStop, or TargetExposure, mirroring
// strategy.IntentFactory's own method names.
type DescribedIntent struct {
	Kind             order.IntentKind
	Instrument       instrument.ID
	Side             order.Side
	Quantity         *num.Quantity
	StopPrice        *num.Price
	CorrelationToken string
}

// Enter builds a DescribedIntent expressing "open or increase a
// position in side's direction" — sdk's own counterpart to
// strategy.IntentFactory.Enter.
func Enter(instID instrument.ID, side order.Side) DescribedIntent {
	return DescribedIntent{Kind: order.IntentEnter, Instrument: instID, Side: side}
}

// Exit builds a DescribedIntent expressing "remove whatever exposure
// currently exists for instID" — sdk's own counterpart to
// strategy.IntentFactory.Exit.
func Exit(instID instrument.ID) DescribedIntent {
	return DescribedIntent{Kind: order.IntentExit, Instrument: instID}
}

// AdjustStop builds a DescribedIntent expressing "move instID's
// protective stop to stopPrice" — sdk's own counterpart to
// strategy.IntentFactory.AdjustStop.
func AdjustStop(instID instrument.ID, stopPrice num.Price) DescribedIntent {
	return DescribedIntent{Kind: order.IntentAdjustStop, Instrument: instID, StopPrice: &stopPrice}
}

// EnterWithStop builds a DescribedIntent expressing "open a position
// in side's direction, with an initial protective stop at stopPrice
// active from the same fill" (ADR-059) — sdk's own
// counterpart to strategy.IntentFactory.EnterWithStop.
func EnterWithStop(instID instrument.ID, side order.Side, stopPrice num.Price) DescribedIntent {
	return DescribedIntent{Kind: order.IntentEnterWithStop, Instrument: instID, Side: side, StopPrice: &stopPrice}
}

// TargetExposure builds a DescribedIntent expressing "reach exactly
// this position" — sdk's own counterpart to
// strategy.IntentFactory.TargetExposure.
func TargetExposure(instID instrument.ID, side order.Side, quantity num.Quantity) DescribedIntent {
	return DescribedIntent{Kind: order.IntentTargetExposure, Instrument: instID, Side: side, Quantity: &quantity}
}

// WithCorrelation returns a copy of d carrying token — an opaque,
// guest-chosen string with no identity meaning of its own beyond
// grouping: the host reuses one real, freshly minted
// id.CorrelationID (via its own retained IntentFactory) across every
// DescribedIntent sharing the same token within one OnBarResponse,
// mirroring strategy.IntentFactory.WithCorrelation's own grouping
// behavior without a guest ever touching a real CorrelationID itself
// (strategy.proto's own DescribedIntent doc comment).
func (d DescribedIntent) WithCorrelation(token string) DescribedIntent {
	d.CorrelationToken = token
	return d
}
