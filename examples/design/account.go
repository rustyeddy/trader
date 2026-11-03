package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"math"
)

// money is an amount of the account currency in cents (see moneyScale).
type money int64

type account struct {
	id      string
	balance money
	riskPct rate // fraction of the account risked per trade

	orders    map[string][]*order // submitted orders awaiting execution, by instrument
	positions map[string]*position

	broker *broker
}

func newAccount(id string, balance money) *account {
	return &account{
		id:        id,
		balance:   balance,
		riskPct:   rateOnePercent, // default risk per trade
		orders:    make(map[string][]*order),
		positions: make(map[string]*position),
	}
}

// equity is what the risk budget is a fraction of: the balance plus the
// unrealized profit and loss of the open positions.
func (act *account) equity() (money, error) {
	pnl, err := act.unrealizedPnL()
	if err != nil {
		return 0, err
	}
	return checkedAdd(act.balance, pnl)
}

// unrealizedPnL is the profit or loss of every open position at its latest
// mark. A position is marked at its fill price when it opens, so a mark always
// exists and an unmarked position is never counted as zero.
func (act *account) unrealizedPnL() (money, error) {
	var total money
	for _, pos := range act.positions {
		pnl, err := pos.unrealizedPnL()
		if err != nil {
			return 0, fmt.Errorf("unrealized P/L of %s: %w", pos.instrument, err)
		}
		total, err = checkedAdd(total, pnl)
		if err != nil {
			return 0, fmt.Errorf("unrealized P/L total: %w", err)
		}
	}
	return total, nil
}

// marginAvailable is what new positions may be margined against. It is the
// balance for now; once positions exist it becomes equity less the margin
// already in use. Sizing keeps it separate from equity: risk is measured
// against equity, the margin limit against marginAvailable.
func (act *account) marginAvailable() money {
	return act.balance
}

// moneyScale is how many money units make one unit of the account currency:
// money counts cents, so profit and loss are exact to the penny.
//
// TODO: the account currency is assumed to be USD, and an instrument's quote
// currency is assumed to be USD too, so no conversion is done. Convert other
// quote currencies, and account for spread and commission, before P/L is
// treated as the account's real result.
const moneyScale int64 = 100

// usd is whole dollars as money.
func usd(dollars int64) money { return money(dollars * moneyScale) }

// errUnsupportedAction is returned for actions the account cannot size yet.
// It is a programming gap, not a trading outcome.
var errUnsupportedAction = errors.New("action not supported yet")

// size decides how many units the order trades, or rejects it. The strategy
// says what it wants; the account decides how much.
//
//   - open: sized to the smaller of the risk limit (a stop-out loses at most
//     riskPct of the balance) and the margin limit (see sizeOpen), or rejected
//     with errInsufficientMargin or errOrderTooSmall if that is below the
//     instrument's minimum.
//   - close: units stay 0, meaning the whole position, which is resolved once
//     positions exist.
//   - add, reduce and anything else: errUnsupportedAction until their size
//     rules are defined.
func (act *account) size(o *order) error {
	inst, ok := getInstrument(o.instrument)
	if !ok {
		return fmt.Errorf("size %s: %w: %q", o.action, errUnknownInstrument, o.instrument)
	}

	switch o.action {
	case open:
		if err := act.sizeOpen(o, inst); err != nil {
			return err
		}
	case close:
		// whole position
	default:
		return fmt.Errorf("size %s: %w", o.action, errUnsupportedAction)
	}
	o.record(stateSized, "")
	return nil
}

// sizeOpen sets o.units for an opening order.
//
// The strategy decides where the stop goes; the account only decides how much
// to trade given it. An opening order without a valid stop is an invalid
// order, not a rejection. Two limits apply, and the smaller one wins:
//
//	riskUnits   = floor(equity * riskPct * priceScale / (rateScale * stopDistance))
//	marginUnits = floor(marginAvailable * priceScale * rateScale / (price * marginRate))
//
// riskUnits is the most that loses at most riskPct of equity if stopped
// out; marginUnits is the most whose margin requirement (units * price *
// marginRate, rounded up) the available margin can cover. Each is a single
// rounding down of an exact integer ratio. A margin shortfall therefore makes
// the trade smaller instead of refusing it.
//
// The smaller limit is then rounded down to the instrument's step and capped at
// its maximum. Only if the result is below the instrument's minimum is the
// order rejected: with errInsufficientMargin when margin was the limit, and
// with errOrderTooSmall when the risk budget was.
func (act *account) sizeOpen(o *order, inst *instrument) error {
	if act.marginAvailable() <= 0 {
		return reject(o, errInsufficientMargin)
	}
	if o.price <= 0 {
		return fmt.Errorf("size open: %w: a market order needs a reference price", errInvalidOrder)
	}
	if inst.marginRate <= 0 {
		return fmt.Errorf("size open: %w: %s has no margin rate", errInstrumentConfig, inst.symbol)
	}

	dist, err := stopDistance(o)
	if err != nil {
		return err
	}

	equity, err := act.equity()
	if err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	riskUnits, err := muldiv(
		[]int64{int64(equity), int64(act.riskPct), priceScale},
		[]int64{rateScale, dist, moneyScale},
		false)
	if err != nil {
		return fmt.Errorf("size open: risk units: %w", err)
	}
	marginUnits, err := muldiv(
		[]int64{int64(act.marginAvailable()), priceScale, rateScale},
		[]int64{int64(o.price), int64(inst.marginRate), moneyScale},
		false)
	if err != nil {
		return fmt.Errorf("size open: margin units: %w", err)
	}

	units, marginLimited := riskUnits, false
	if marginUnits < riskUnits {
		units, marginLimited = marginUnits, true
	}
	if inst.unitStep > 1 {
		units -= units % inst.unitStep
	}
	if inst.maxUnits > 0 && units > inst.maxUnits {
		units = inst.maxUnits
		if inst.unitStep > 1 {
			units -= units % inst.unitStep
		}
	}

	if min := max(inst.minUnits, 1); units < min {
		if marginLimited {
			return reject(o, fmt.Errorf("%w: margin covers %d units, minimum %d",
				errInsufficientMargin, units, min))
		}
		return reject(o, fmt.Errorf("%w: %d units, minimum %d", errOrderTooSmall, units, min))
	}

	o.units = units
	return nil
}

// stopDistance returns how far, in price points, the order's stop is from its
// entry price. A stop the strategy did not set, or one that is not beyond the
// entry on the losing side, is a strategy bug and an invalid order.
func stopDistance(o *order) (int64, error) {
	if o.stopLoss == 0 {
		return 0, fmt.Errorf("size open: %w: an open order needs a stop loss", errInvalidOrder)
	}
	dist := int64(o.price - o.stopLoss)
	if o.direction == short {
		dist = int64(o.stopLoss - o.price)
	}
	if dist <= 0 {
		return 0, fmt.Errorf("size open: %w: %s stop %d is not beyond the entry price %d",
			errInvalidOrder, o.direction, o.stopLoss, o.price)
	}
	return dist, nil
}

// reject records the rejection on the order and returns err.
func reject(o *order, err error) error {
	o.record(stateRejected, err.Error())
	return err
}

var errSizeOverflow = errors.New("amount overflows int64")

// checkedAdd adds two money amounts without silently wrapping int64.
func checkedAdd(a, b money) (money, error) {
	if (b > 0 && a > money(math.MaxInt64)-b) ||
		(b < 0 && a < money(math.MinInt64)-b) {
		return 0, errSizeOverflow
	}
	return a + b, nil
}

// muldiv returns the product of nums divided by the product of dens, rounded
// down or, with roundUp, up. It multiplies everything before dividing, in
// arbitrary precision, so there is a single rounding and no overflow.
func muldiv(nums, dens []int64, roundUp bool) (int64, error) {
	n, d := big.NewInt(1), big.NewInt(1)
	for _, v := range nums {
		n.Mul(n, big.NewInt(v))
	}
	for _, v := range dens {
		d.Mul(d, big.NewInt(v))
	}
	if d.Sign() == 0 {
		return 0, errors.New("division by zero")
	}

	// QuoRem truncates toward zero, which is the ceiling of a negative quotient
	// and the floor of a positive one.
	q, r := new(big.Int).QuoRem(n, d, new(big.Int))
	if r.Sign() != 0 {
		negative := (n.Sign() < 0) != (d.Sign() < 0)
		switch {
		case negative && !roundUp:
			q.Sub(q, big.NewInt(1))
		case !negative && roundUp:
			q.Add(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() {
		return 0, errSizeOverflow
	}
	return q.Int64(), nil
}

// onBar lets the broker execute the orders waiting on this bar's instrument.
// Orders that are no longer submitted (filled or rejected) leave the list.
func (a *account) onBar(ctx context.Context, b bar) error {
	pending := append([]*order(nil), a.orders[b.instrument]...)

	for _, o := range pending {
		if err := a.broker.processOrder(ctx, b, o); err != nil {
			return err
		}
	}

	// Value the open position at this bar's close, after any fill on its open.
	if pos := a.positions[b.instrument]; pos != nil {
		pos.markPrice = b.c
	}

	waiting := a.orders[b.instrument][:0]
	for _, o := range a.orders[b.instrument] {
		if o.state == stateSubmitted {
			waiting = append(waiting, o)
		}
	}
	a.orders[b.instrument] = waiting
	return nil
}

// applyFill updates the instrument's position for a filled order. One position
// per instrument (netting): an open creates it, a close removes it and the
// order takes the closed size. Add and reduce wait on the size type.
func (a *account) applyFill(o *order) error {
	pos := a.positions[o.instrument]
	switch o.action {
	case open:
		if pos != nil {
			return fmt.Errorf("%w: %s is already %s", errPositionExists, o.instrument, pos.direction)
		}
		a.positions[o.instrument] = &position{
			instrument: o.instrument,
			direction:  o.direction,
			units:      o.units,
			entryPrice: o.fillPrice,
			markPrice:  o.fillPrice,
		}
	case close:
		if pos == nil {
			return fmt.Errorf("%w: %s", errNoPosition, o.instrument)
		}

		pnl, err := pos.realizedPnL(o.fillPrice)
		if err != nil {
			return err
		}
		balance, err := checkedAdd(a.balance, pnl)
		if err != nil {
			return fmt.Errorf("close %s balance: %w", o.instrument, err)
		}
		a.balance = balance
		o.units = pos.units

		delete(a.positions, o.instrument)
	default:
		return fmt.Errorf("fill %s: %w", o.action, errUnsupportedAction)
	}
	return nil
}

// realizedPnL is the profit or loss, in cents, of closing the position at exit.
// It rounds down (toward a larger loss) once, to the cent.
//
// TODO: this assumes the quote currency is the account currency (USD) and
// excludes spread and commission; see moneyScale.
func (p *position) realizedPnL(exit price) (money, error) {
	return p.pnlAt(exit)
}

// unrealizedPnL is the profit or loss, in cents, of the position at its latest
// mark. The same TODO as realizedPnL applies.
func (p *position) unrealizedPnL() (money, error) {
	return p.pnlAt(p.markPrice)
}

// pnlAt is the profit or loss, in cents, of the position valued at p.
func (p *position) pnlAt(exit price) (money, error) {
	// Compute the price difference before narrowing: subtraction can overflow
	// int64 even when the final P/L is representable.
	delta := new(big.Int).Sub(big.NewInt(int64(exit)), big.NewInt(int64(p.entryPrice)))
	if p.direction == short {
		delta.Neg(delta)
	}
	amount := delta.Mul(delta, big.NewInt(p.units))
	amount.Mul(amount, big.NewInt(moneyScale))
	// Div rounds toward negative infinity, preserving the cent policy.
	amount.Div(amount, big.NewInt(priceScale))
	if !amount.IsInt64() {
		return 0, errSizeOverflow
	}
	return money(amount.Int64()), nil
}
