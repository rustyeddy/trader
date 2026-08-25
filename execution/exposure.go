package execution

import (
	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// findPosition returns acc's open position in listing's instrument, if
// any. Position matching is by instrument identity, not exact Listing
// equality: Intent (and therefore the position it may already own)
// identifies an instrument, never a venue-specific Listing (#177).
func findPosition(acc account.Snapshot, listing instrument.Listing) (order.Position, bool) {
	for _, p := range acc.Positions() {
		if p.Listing.InstrumentID().Equal(listing.InstrumentID()) {
			return p, true
		}
	}
	return order.Position{}, false
}

// planExit determines the Side and Quantity that would close acc's
// entire current position in listing, or ErrNoPositionToExit if there
// is nothing to close.
func planExit(acc account.Snapshot, listing instrument.Listing) (order.Side, num.Quantity, error) {
	pos, ok := findPosition(acc, listing)
	if !ok {
		return 0, num.Quantity{}, ErrNoPositionToExit
	}
	switch pos.Side {
	case order.Long:
		return order.Sell, pos.Quantity, nil
	case order.Short:
		return order.Buy, pos.Quantity, nil
	default: // order.Flat: a stored Position is never Flat (see account.Snapshot's own invariant), but handled explicitly rather than falling through silently.
		return 0, num.Quantity{}, ErrNoPositionToExit
	}
}

// planTargetExposure determines the Side, Quantity, and ReduceOnly
// value execution needs to move acc's current position in listing to
// exactly targetSide/targetQty, the absolute desired exposure
// (#177: IntentTargetExposure carries a target, not a delta).
//
//   - No current position: propose targetSide/targetQty outright.
//   - Same direction, growing (target > current): propose the
//     additional targetSide/delta needed.
//   - Same direction, shrinking (target < current): propose a
//     ReduceOnly opposite-side/delta order — a partial exit, not a
//     reversal.
//   - Same direction, unchanged: ErrAlreadyAtTarget.
//   - Opposite direction: propose targetSide sized at current+target,
//     crossing through flat to reach the new target in one order.
func planTargetExposure(acc account.Snapshot, listing instrument.Listing, targetSide order.Side, targetQty num.Quantity) (order.Side, num.Quantity, bool, error) {
	pos, ok := findPosition(acc, listing)
	if !ok || pos.Side == order.Flat {
		return targetSide, targetQty, false, nil
	}

	sameDirection := (pos.Side == order.Long && targetSide == order.Buy) ||
		(pos.Side == order.Short && targetSide == order.Sell)

	if !sameDirection {
		total, err := pos.Quantity.Add(targetQty)
		if err != nil {
			return 0, num.Quantity{}, false, err
		}
		return targetSide, total, false, nil
	}

	switch pos.Quantity.Cmp(targetQty) {
	case 0:
		return 0, num.Quantity{}, false, ErrAlreadyAtTarget
	case -1: // current < target: grow in the same direction
		delta, err := targetQty.Sub(pos.Quantity)
		if err != nil {
			return 0, num.Quantity{}, false, err
		}
		return targetSide, delta, false, nil
	default: // current > target: partial reduce, opposite side, reduce-only
		delta, err := pos.Quantity.Sub(targetQty)
		if err != nil {
			return 0, num.Quantity{}, false, err
		}
		return opposite(targetSide), delta, true, nil
	}
}

func opposite(s order.Side) order.Side {
	if s == order.Buy {
		return order.Sell
	}
	return order.Buy
}
