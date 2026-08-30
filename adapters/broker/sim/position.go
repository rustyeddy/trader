package sim

import (
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

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
