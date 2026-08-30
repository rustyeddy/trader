package order

import (
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// PositionTransition classifies what applying one fill did to a
// position, so a caller does not have to reverse-engineer the
// transition by diffing the old and new Position itself.
type PositionTransition uint8

const (
	// TransitionOpen means the fill established a new position from
	// flat.
	TransitionOpen PositionTransition = iota
	// TransitionIncrease means the fill added to an existing
	// same-side position.
	TransitionIncrease
	// TransitionReduce means the fill partially closed an existing
	// position without flattening it.
	TransitionReduce
	// TransitionClose means the fill exactly flattened an existing
	// position.
	TransitionClose
	// TransitionReverse means the fill closed the entire existing
	// position and opened a new one in the opposite direction with
	// the remaining quantity.
	TransitionReverse
)

// String returns a human-readable PositionTransition name.
func (t PositionTransition) String() string {
	switch t {
	case TransitionOpen:
		return "open"
	case TransitionIncrease:
		return "increase"
	case TransitionReduce:
		return "reduce"
	case TransitionClose:
		return "close"
	case TransitionReverse:
		return "reverse"
	default:
		return fmt.Sprintf("PositionTransition(%d)", uint8(t))
	}
}

// PositionTransitionResult is the outcome of applying one fill to a
// position: the resulting Position, the realized-PnL delta that fill
// produced, and which of the five transitions occurred.
type PositionTransitionResult struct {
	Position    Position
	RealizedPnL num.Money
	Transition  PositionTransition
}

// positionSideFor reports the PositionSide a fill on side would open
// or add to: Buy opens/adds to a Long position, Sell a Short one.
func positionSideFor(side Side) PositionSide {
	if side == Sell {
		return Short
	}
	return Long
}

// clonePrice returns a defensive copy of p, safe to store in a new
// Position independent of whatever value p itself came from.
func clonePrice(p num.Price) *num.Price {
	v := p
	return &v
}

// ApplyFillToPosition computes the Position and realized-PnL delta
// that result from filling side/price/quantity against existing, the
// current position for listing (the zero Position if hasExisting is
// false). currency is the listing's quote/settlement currency; every
// Money value in and out of this function is denominated in it —
// ApplyFillToPosition has no FX conversion-rate source, so a caller
// combining fills across a different account home currency must
// convert before calling it.
//
// This is pure, broker-neutral position accounting (originally
// implemented inside the sim broker adapter for issue #152/M3-09, and
// extracted here so it has exactly one implementation both a broker
// adapter and after-the-fact trade derivation can share — see issue
// #217/M5-09). It carries no cash, margin, commission, or other
// account-level side effect: those remain the calling adapter's own
// responsibility, applied to its own account state using this
// function's RealizedPnL result.
//
// Five transitions, matching #152's acceptance criteria exactly:
//
//   - Open (no existing position, or existing is Flat): the new
//     Position's AvgPrice is price directly. No realized PnL.
//   - Increase (existing position, same side as the fill): AvgPrice
//     becomes the quantity-weighted average of the existing and new
//     notional (Money.DivQuantity, ADR-027). No realized PnL.
//   - Reduce (opposite side, fillQty < existing.Quantity): AvgPrice is
//     unchanged; realized PnL is the notional difference between price
//     and AvgPrice over the closed quantity, signed by which side
//     benefits from a higher exit price (Long) or a lower one (Short).
//   - Close (opposite side, fillQty == existing.Quantity): same PnL
//     calculation as Reduce, over the full quantity; the resulting
//     Position is Flat.
//   - Reverse (opposite side, fillQty > existing.Quantity): closes the
//     entire existing position (same PnL calculation as Close, over
//     existing.Quantity), then opens a new position in the opposite
//     direction at price for the remaining quantity.
//
// Every Money arithmetic step here uses num's exact, checked
// operations (Price.MulQuantity, Money.Add/Sub, Money.DivQuantity); no
// local floating-point or unchecked arithmetic. A currency mismatch —
// which should not occur for a single listing's own settlement
// currency — surfaces as num.ErrCurrencyMismatch rather than being
// silently combined.
func ApplyFillToPosition(existing Position, hasExisting bool, accountID id.AccountID, listing instrument.Listing, currency num.Currency, side Side, price num.Price, quantity num.Quantity) (PositionTransitionResult, error) {
	zero, err := num.ParseMoney("0", currency)
	if err != nil {
		return PositionTransitionResult{}, err
	}

	fillSide := positionSideFor(side)

	if !hasExisting || existing.Side == Flat {
		pos, err := NewPosition(Position{
			AccountID: accountID,
			Listing:   listing,
			Side:      fillSide,
			Quantity:  quantity,
			AvgPrice:  clonePrice(price),
		})
		return PositionTransitionResult{Position: pos, RealizedPnL: zero, Transition: TransitionOpen}, err
	}

	if existing.Side == fillSide {
		pos, err := increasePosition(existing, accountID, listing, currency, price, quantity)
		return PositionTransitionResult{Position: pos, RealizedPnL: zero, Transition: TransitionIncrease}, err
	}
	return reduceOrReversePosition(existing, accountID, listing, currency, fillSide, price, quantity)
}

// increasePosition adds quantity at price to an existing same-side
// position, recomputing AvgPrice as the quantity-weighted average.
func increasePosition(existing Position, accountID id.AccountID, listing instrument.Listing, currency num.Currency, price num.Price, quantity num.Quantity) (Position, error) {
	existingNotional, err := existing.AvgPrice.MulQuantity(existing.Quantity, currency)
	if err != nil {
		return Position{}, err
	}
	addNotional, err := price.MulQuantity(quantity, currency)
	if err != nil {
		return Position{}, err
	}
	totalNotional, err := existingNotional.Add(addNotional)
	if err != nil {
		return Position{}, err
	}
	totalQty, err := existing.Quantity.Add(quantity)
	if err != nil {
		return Position{}, err
	}
	newAvg, err := totalNotional.DivQuantity(totalQty)
	if err != nil {
		return Position{}, err
	}

	return NewPosition(Position{
		AccountID: accountID,
		Listing:   listing,
		Side:      existing.Side,
		Quantity:  totalQty,
		AvgPrice:  &newAvg,
	})
}

// reduceOrReversePosition applies an opposite-side fill against
// existing: a reduce or exact close if quantity <= existing.Quantity,
// or a reversal (close then reopen in fillSide) if quantity is larger.
func reduceOrReversePosition(existing Position, accountID id.AccountID, listing instrument.Listing, currency num.Currency, fillSide PositionSide, price num.Price, quantity num.Quantity) (PositionTransitionResult, error) {
	closedQty := quantity
	if existing.Quantity.Cmp(closedQty) < 0 {
		closedQty = existing.Quantity
	}

	exitNotional, err := price.MulQuantity(closedQty, currency)
	if err != nil {
		return PositionTransitionResult{}, err
	}
	entryNotional, err := existing.AvgPrice.MulQuantity(closedQty, currency)
	if err != nil {
		return PositionTransitionResult{}, err
	}

	var realizedPnL num.Money
	if existing.Side == Long {
		realizedPnL, err = exitNotional.Sub(entryNotional)
	} else {
		realizedPnL, err = entryNotional.Sub(exitNotional)
	}
	if err != nil {
		return PositionTransitionResult{}, err
	}

	switch quantity.Cmp(existing.Quantity) {
	case -1: // reduce
		remaining, err := existing.Quantity.Sub(quantity)
		if err != nil {
			return PositionTransitionResult{}, err
		}
		pos, err := NewPosition(Position{
			AccountID: accountID,
			Listing:   listing,
			Side:      existing.Side,
			Quantity:  remaining,
			AvgPrice:  clonePrice(*existing.AvgPrice),
		})
		return PositionTransitionResult{Position: pos, RealizedPnL: realizedPnL, Transition: TransitionReduce}, err

	case 0: // exact close
		pos, err := NewPosition(Position{AccountID: accountID, Listing: listing, Side: Flat})
		return PositionTransitionResult{Position: pos, RealizedPnL: realizedPnL, Transition: TransitionClose}, err

	default: // reverse
		remaining, err := quantity.Sub(existing.Quantity)
		if err != nil {
			return PositionTransitionResult{}, err
		}
		pos, err := NewPosition(Position{
			AccountID: accountID,
			Listing:   listing,
			Side:      fillSide,
			Quantity:  remaining,
			AvgPrice:  clonePrice(price),
		})
		return PositionTransitionResult{Position: pos, RealizedPnL: realizedPnL, Transition: TransitionReverse}, err
	}
}
