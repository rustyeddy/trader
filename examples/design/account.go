package main

import (
	"errors"
	"fmt"
	"math"
	"math/big"
)

// money is an amount of the account currency in cents (see moneyScale).
type money int64

type account struct {
	id      string
	balance money
	riskPct rate // fraction of the account risked per trade

	positions map[string]*position
}

func newAccount(id string, balance money) *account {
	return &account{
		id:        id,
		balance:   balance,
		riskPct:   rateOnePercent, // default risk per trade
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

// marginAvailable is what new positions may be margined against: equity less
// the margin already in use. Sizing keeps it separate from equity: risk is
// measured against equity, the margin limit against marginAvailable. It can be
// zero or negative, which leaves no room for a new position.
func (act *account) marginAvailable() (money, error) {
	eq, err := act.equity()
	if err != nil {
		return 0, err
	}
	used, err := act.usedMargin()
	if err != nil {
		return 0, err
	}
	return checkedAdd(eq, -used) // used is never negative
}

// usedMargin is the margin held against open positions, valued at their
// latest mark. Margin reserved by orders that are submitted but not yet filled
// is the broker's to know, since it holds the orders; it passes that to size.
//
// TODO: the margin is in the quote currency, assumed to be the account
// currency (USD); see moneyScale.
func (act *account) usedMargin() (money, error) {
	var total money
	for _, pos := range act.positions {
		m, err := marginFor(pos.instrument, pos.units, pos.markPrice)
		if err != nil {
			return 0, err
		}
		if total, err = checkedAdd(total, m); err != nil {
			return 0, fmt.Errorf("used margin total: %w", err)
		}
	}
	return total, nil
}

// marginFor is the margin units of symbol need at price p, rounded up to the
// cent: units * p * marginRate.
func marginFor(symbol string, units int64, p price) (money, error) {
	inst, ok := getInstrument(symbol)
	if !ok {
		return 0, fmt.Errorf("margin for %s: %w", symbol, errUnknownInstrument)
	}
	m, err := muldiv(
		[]int64{units, int64(p), int64(inst.marginRate), moneyScale},
		[]int64{priceScale, rateScale},
		true)
	if err != nil {
		return 0, fmt.Errorf("margin for %s: %w", symbol, err)
	}
	return money(m), nil
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
func (act *account) size(o *order, reserved money) error {
	inst, ok := getInstrument(o.instrument)
	if !ok {
		return fmt.Errorf("size %s: %w: %q", o.action, errUnknownInstrument, o.instrument)
	}

	switch o.action {
	case open:
		if err := act.sizeOpen(o, inst, reserved); err != nil {
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
//	marginAvailable = equity - usedMargin - reserved
//
// reserved is the margin the broker holds for this account's submitted opens
// that have not filled yet; without it, orders sized before they fill would
// each see the same margin available.
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
func (act *account) sizeOpen(o *order, inst *instrument, reserved money) error {
	margin, err := act.marginAvailable()
	if err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	// held by the broker's submitted opens that have not filled (reserved >= 0)
	if margin, err = checkedAdd(margin, -reserved); err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	if margin <= 0 {
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
		[]int64{int64(margin), priceScale, rateScale},
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

// mark values the open position in b's instrument at the bar's close. The
// broker calls it after the bar's fills, so a position opened at the bar's open
// is valued at its close.
func (a *account) mark(b bar) {
	if pos := a.positions[b.instrument]; pos != nil {
		pos.markPrice = b.c
	}
}

// setBalance is how the broker reports the account's balance after realizing
// profit or loss.
func (a *account) setBalance(m money) {
	a.balance = m
}

// applyFill updates the instrument's position for a filled order and returns
// the profit or loss it realized, which is zero unless it closed a position.
// The broker applies that to the balance. One position per instrument
// (netting): an open creates it, a close removes it and the order takes the
// closed size. Add and reduce wait on the size type.
func (a *account) applyFill(o *order) (money, error) {
	pos := a.positions[o.instrument]
	switch o.action {
	case open:
		if pos != nil {
			return 0, fmt.Errorf("%w: %s is already %s", errPositionExists, o.instrument, pos.direction)
		}
		a.positions[o.instrument] = &position{
			instrument: o.instrument,
			direction:  o.direction,
			units:      o.units,
			entryPrice: o.fillPrice,
			markPrice:  o.fillPrice,
		}
		return 0, nil
	case close:
		if pos == nil {
			return 0, fmt.Errorf("%w: %s", errNoPosition, o.instrument)
		}
		pnl, err := pos.realizedPnL(o.fillPrice)
		if err != nil {
			return 0, err
		}
		// Check the balance can take the P/L before the position is touched, so
		// a failed close leaves the position and the order as they were.
		if _, err := checkedAdd(a.balance, pnl); err != nil {
			return 0, fmt.Errorf("close %s balance: %w", o.instrument, err)
		}
		o.units = pos.units
		delete(a.positions, o.instrument)
		return pnl, nil
	default:
		return 0, fmt.Errorf("fill %s: %w", o.action, errUnsupportedAction)
	}
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
