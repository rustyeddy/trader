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

func (act *account) marginAvailable() money {
	return act.balance
}

// moneyScale is how many money units make one unit of the account currency.
// For now money is whole account-currency units, and the account currency is
// the instrument's quote currency, so no conversion is needed.
const moneyScale int64 = 1

// defaultStopPct places the stop this fraction of the entry price away. It
// stands in for a stop chosen by the strategy.
const defaultStopPct = rateOnePercent

// errUnsupportedAction is returned for actions the account cannot size yet.
// It is a programming gap, not a trading outcome.
var errUnsupportedAction = errors.New("action not supported yet")

// size decides how many units the order trades, or rejects it. The strategy
// says what it wants; the account decides how much.
//
//   - open: sized so that being stopped out loses riskPct of the balance (see
//     sizeOpen), or rejected with errInsufficientMargin or errOrderTooSmall.
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

// sizeOpen sets o.units and o.stopLoss for an opening order.
//
// The stop is the order's own stopLoss when the strategy set one, otherwise
// defaultStopPct of the entry price away on the losing side. Units are then
//
//	floor(balance * riskPct * priceScale / (rateScale * stopDistance))
//
// so a stop-out loses at most riskPct of the balance; all in integers with a
// single rounding down. Units are rounded down to the instrument's step and
// capped at its maximum. The order is rejected if that is below the minimum
// or if the margin required (units * price * marginRate) exceeds the margin
// available.
func (act *account) sizeOpen(o *order, inst *instrument) error {
	if act.marginAvailable() <= 0 {
		return reject(o, errInsufficientMargin)
	}
	if o.price <= 0 {
		return fmt.Errorf("size open: %w: a market order needs a reference price", errInvalidOrder)
	}

	dist, stop, err := stopFor(o)
	if err != nil {
		return err
	}

	units, err := muldiv(
		[]int64{int64(act.marginAvailable()), int64(act.riskPct), priceScale},
		[]int64{rateScale, dist, moneyScale},
		false)
	if err != nil {
		return fmt.Errorf("size open: units: %w", err)
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
		return reject(o, fmt.Errorf("%w: %d units, minimum %d", errOrderTooSmall, units, min))
	}

	required, err := muldiv(
		[]int64{units, int64(o.price), int64(inst.marginRate), moneyScale},
		[]int64{priceScale, rateScale},
		true) // round the requirement up
	if err != nil {
		return fmt.Errorf("size open: margin: %w", err)
	}
	if money(required) > act.marginAvailable() {
		return reject(o, fmt.Errorf("%w: need %d, have %d", errInsufficientMargin, required, act.marginAvailable()))
	}

	o.units = units
	o.stopLoss = stop
	return nil
}

// stopFor returns the stop distance in price points and the stop price for an
// opening order, validating a stop the strategy supplied.
func stopFor(o *order) (dist int64, stop price, err error) {
	if o.stopLoss == 0 {
		dist = int64(o.price) * int64(defaultStopPct) / rateScale
		if dist < 1 {
			dist = 1
		}
		if o.direction == short {
			return dist, o.price + price(dist), nil
		}
		return dist, o.price - price(dist), nil
	}

	if o.direction == short {
		dist = int64(o.stopLoss - o.price)
	} else {
		dist = int64(o.price - o.stopLoss)
	}
	if dist <= 0 {
		return 0, 0, fmt.Errorf("size open: %w: %s stop %d is not beyond the entry price %d",
			errInvalidOrder, o.direction, o.stopLoss, o.price)
	}
	return dist, o.stopLoss, nil
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
