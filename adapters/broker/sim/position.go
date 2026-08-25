package sim

import (
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// positionSideFor reports the PositionSide a fill on side would open
// or add to: Buy opens/adds to a Long position, Sell a Short one.
func positionSideFor(side order.Side) order.PositionSide {
	if side == order.Sell {
		return order.Short
	}
	return order.Long
}

// clonePrice returns a defensive copy of p, safe to store in a new
// Position independent of whatever value p itself came from.
func clonePrice(p num.Price) *num.Price {
	v := p
	return &v
}

// applyFillToPosition computes the Position and realized-PnL delta
// (issue #152, M3-09) that result from filling side/price/quantity
// against existing, the account's current position for listing (the
// zero Position if hasExisting is false). currency is the listing's
// quote/settlement currency (instrument.Spec.SettlementCurrency) —
// every Money value in and out of this function is denominated in it;
// composing with an account's own home currency, when it differs, is
// the caller's job (see accountState.snapshotLocked's Equity/RealizedPnL
// composition, and ADR-004's Money.Convert).
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
func applyFillToPosition(existing order.Position, hasExisting bool, accountID id.AccountID, listing instrument.Listing, currency num.Currency, side order.Side, price num.Price, quantity num.Quantity) (order.Position, num.Money, error) {
	zero, err := zeroMoney(currency)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}

	fillSide := positionSideFor(side)

	if !hasExisting || existing.Side == order.Flat {
		pos, err := order.NewPosition(order.Position{
			AccountID: accountID,
			Listing:   listing,
			Side:      fillSide,
			Quantity:  quantity,
			AvgPrice:  clonePrice(price),
		})
		return pos, zero, err
	}

	if existing.Side == fillSide {
		return increasePosition(existing, accountID, listing, currency, price, quantity)
	}
	return reduceOrReversePosition(existing, accountID, listing, currency, fillSide, price, quantity)
}

// increasePosition adds quantity at price to an existing same-side
// position, recomputing AvgPrice as the quantity-weighted average.
func increasePosition(existing order.Position, accountID id.AccountID, listing instrument.Listing, currency num.Currency, price num.Price, quantity num.Quantity) (order.Position, num.Money, error) {
	zero, err := zeroMoney(currency)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}

	existingNotional, err := existing.AvgPrice.MulQuantity(existing.Quantity, currency)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}
	addNotional, err := price.MulQuantity(quantity, currency)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}
	totalNotional, err := existingNotional.Add(addNotional)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}
	totalQty, err := existing.Quantity.Add(quantity)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}
	newAvg, err := totalNotional.DivQuantity(totalQty)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}

	pos, err := order.NewPosition(order.Position{
		AccountID: accountID,
		Listing:   listing,
		Side:      existing.Side,
		Quantity:  totalQty,
		AvgPrice:  &newAvg,
	})
	return pos, zero, err
}

// reduceOrReversePosition applies an opposite-side fill against
// existing: a reduce or exact close if quantity <= existing.Quantity,
// or a reversal (close then reopen in fillSide) if quantity is larger.
func reduceOrReversePosition(existing order.Position, accountID id.AccountID, listing instrument.Listing, currency num.Currency, fillSide order.PositionSide, price num.Price, quantity num.Quantity) (order.Position, num.Money, error) {
	closedQty := quantity
	if existing.Quantity.Cmp(closedQty) < 0 {
		closedQty = existing.Quantity
	}

	exitNotional, err := price.MulQuantity(closedQty, currency)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}
	entryNotional, err := existing.AvgPrice.MulQuantity(closedQty, currency)
	if err != nil {
		return order.Position{}, num.Money{}, err
	}

	var realizedPnL num.Money
	if existing.Side == order.Long {
		realizedPnL, err = exitNotional.Sub(entryNotional)
	} else {
		realizedPnL, err = entryNotional.Sub(exitNotional)
	}
	if err != nil {
		return order.Position{}, num.Money{}, err
	}

	switch quantity.Cmp(existing.Quantity) {
	case -1: // reduce
		remaining, err := existing.Quantity.Sub(quantity)
		if err != nil {
			return order.Position{}, num.Money{}, err
		}
		pos, err := order.NewPosition(order.Position{
			AccountID: accountID,
			Listing:   listing,
			Side:      existing.Side,
			Quantity:  remaining,
			AvgPrice:  clonePrice(*existing.AvgPrice),
		})
		return pos, realizedPnL, err

	case 0: // exact close
		pos, err := order.NewPosition(order.Position{AccountID: accountID, Listing: listing, Side: order.Flat})
		return pos, realizedPnL, err

	default: // reverse
		remaining, err := quantity.Sub(existing.Quantity)
		if err != nil {
			return order.Position{}, num.Money{}, err
		}
		pos, err := order.NewPosition(order.Position{
			AccountID: accountID,
			Listing:   listing,
			Side:      fillSide,
			Quantity:  remaining,
			AvgPrice:  clonePrice(price),
		})
		return pos, realizedPnL, err
	}
}

// applyCommission debits commission from cash and adds it to
// cumulative fees, both denominated in commission's own currency
// (issue #152, M3-09). It is only ever invoked when a fill actually
// reports a non-nil Commission — this package builds no commission
// model of its own (see order.Fill.Commission's doc comment) — but the
// logic is unconditional here: a commission denominated differently
// from cash/fees is rejected via num.ErrCurrencyMismatch rather than
// silently combined, since Trader has no general FX conversion policy
// for fees.
func applyCommission(cash, fees num.Money, commission num.Money) (num.Money, num.Money, error) {
	newCash, err := cash.Sub(commission)
	if err != nil {
		return num.Money{}, num.Money{}, err
	}
	newFees, err := fees.Add(commission)
	if err != nil {
		return num.Money{}, num.Money{}, err
	}
	return newCash, newFees, nil
}

// unrealizedPnLForPosition returns pos's unrealized PnL given mark,
// the last known price for pos.Listing, denominated in currency.
func unrealizedPnLForPosition(pos order.Position, mark num.Price, currency num.Currency) (num.Money, error) {
	markNotional, err := mark.MulQuantity(pos.Quantity, currency)
	if err != nil {
		return num.Money{}, err
	}
	avgNotional, err := pos.AvgPrice.MulQuantity(pos.Quantity, currency)
	if err != nil {
		return num.Money{}, err
	}
	if pos.Side == order.Long {
		return markNotional.Sub(avgNotional)
	}
	return avgNotional.Sub(markNotional)
}
