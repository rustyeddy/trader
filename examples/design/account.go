package main

import (
	"errors"
	"fmt"
	"math/big"
)

type money int64

type account struct {
	id        string
	balance   money
	riskPct   rate // fraction of the account risked per trade
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

// equity is what the risk budget is a fraction of. It is the balance for now;
// once positions exist it becomes balance plus unrealized profit and loss.
func (act *account) equity() money {
	return act.balance
}

// marginAvailable is what new positions may be margined against. It is the
// balance for now; once positions exist it becomes equity less the margin
// already in use. Sizing keeps it separate from equity: risk is measured
// against equity, the margin limit against marginAvailable.
func (act *account) marginAvailable() money {
	return act.balance
}

// moneyScale is how many money units make one unit of the account currency.
// For now money is whole account-currency units, and the account currency is
// the instrument's quote currency, so no conversion is needed.
const moneyScale int64 = 1

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

	riskUnits, err := muldiv(
		[]int64{int64(act.equity()), int64(act.riskPct), priceScale},
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
