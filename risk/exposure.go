package risk

import (
	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// findPosition returns acc's open position in listing's instrument, if
// any. Position matching is by instrument identity, not exact Listing
// equality — the same convention execution.findPosition uses (#179) —
// but reimplemented locally here rather than imported: risk must never
// import execution (ADR-006, package-boundaries.org). Identical to
// pertradeloss.go's own copy; kept small enough that sharing it across
// files in this package is simpler than extracting a third home for
// one six-line loop.
func findPosition(acc account.Snapshot, listing instrument.Listing) (order.Position, bool) {
	for _, p := range acc.Positions() {
		if p.Listing.InstrumentID().Equal(listing.InstrumentID()) {
			return p, true
		}
	}
	return order.Position{}, false
}

// resultingPosition computes the position that would exist in
// proposal.Listing's instrument after acc accepted proposal — the
// shared basis every position/exposure limit rule in this file
// evaluates against, rather than each rule reimplementing its own
// increase/reduce/close/reversal arithmetic (review feedback on
// #183).
//
// proposal.ReduceOnly is honored explicitly, not left to arithmetic:
// a ReduceOnly proposal can only ever shrink toward Flat, by its own
// documented meaning (order.Proposal's own doc comment) — never open
// fresh exposure or reverse through Flat into the opposite side, even
// if its Quantity numerically exceeds the current position (modeled
// as clamping at Flat, since a real broker would reject or partially
// fill such an order rather than reverse it). A ReduceOnly proposal
// against no position, or in the same direction as the current one
// (a contradiction — reducing never means adding), leaves the current
// position unchanged.
//
// A non-ReduceOnly proposal in the opposite direction from the
// current position that exactly matches or falls short of its
// quantity is a (possibly partial) reduction; one that exceeds it is
// a reversal, resulting in the opposite side sized at the excess.
func resultingPosition(acc account.Snapshot, proposal order.Proposal) (order.PositionSide, num.Quantity, error) {
	pos, hasPosition := findPosition(acc, proposal.Listing)
	curSide := order.Flat
	curQty := num.Quantity{}
	if hasPosition {
		curSide, curQty = pos.Side, pos.Quantity
	}

	sameDirection := curSide != order.Flat &&
		((curSide == order.Long && proposal.Side == order.Buy) ||
			(curSide == order.Short && proposal.Side == order.Sell))

	if proposal.ReduceOnly {
		if curSide == order.Flat || sameDirection {
			return curSide, curQty, nil
		}
		if proposal.Quantity.Cmp(curQty) >= 0 {
			return order.Flat, num.Quantity{}, nil
		}
		newQty, err := curQty.Sub(proposal.Quantity)
		return curSide, newQty, err
	}

	if curSide == order.Flat {
		return positionSideFor(proposal.Side), proposal.Quantity, nil
	}
	if sameDirection {
		newQty, err := curQty.Add(proposal.Quantity)
		return curSide, newQty, err
	}

	switch proposal.Quantity.Cmp(curQty) {
	case -1:
		newQty, err := curQty.Sub(proposal.Quantity)
		return curSide, newQty, err
	case 0:
		return order.Flat, num.Quantity{}, nil
	default:
		excess, err := proposal.Quantity.Sub(curQty)
		return oppositeSide(curSide), excess, err
	}
}

// positionSideFor maps an order.Side to the order.PositionSide opening
// it would establish from Flat.
func positionSideFor(s order.Side) order.PositionSide {
	if s == order.Buy {
		return order.Long
	}
	return order.Short
}

// oppositeSide returns the opposite of a non-Flat order.PositionSide.
func oppositeSide(s order.PositionSide) order.PositionSide {
	if s == order.Long {
		return order.Short
	}
	return order.Long
}
