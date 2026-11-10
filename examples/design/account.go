package main

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"
)

// money is an amount of the account currency in cents (see moneyScale).
type money int64

type account struct {
	id      string
	balance money
	riskPct rate // fraction of the account risked per trade

	positions map[string]*position
	trades    []trade // closed positions, in the order they closed

	closedOutAt time.Time // availability of the frame of the latest margin closeout; zero if none
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
func (act *account) equity() money {
	return act.balance + act.unrealizedPnL()
}

// unrealizedPnL is the profit or loss of every open position at its latest
// mark. A position is marked at its fill price when it opens, so a mark always
// exists and an unmarked position is never counted as zero.
func (act *account) unrealizedPnL() money {
	var total money
	for _, pos := range act.positions {
		total += pos.unrealizedPnL()
	}
	return total
}

// marginAvailable is what new positions may be margined against: equity less
// the margin already in use. Sizing keeps it separate from equity: risk is
// measured against equity, the margin limit against marginAvailable. It can be
// zero or negative, which leaves no room for a new position.
func (act *account) marginAvailable() money {
	return act.equity() - act.usedMargin()
}

// usedMargin is the margin held against open positions, valued at their
// latest mark. Margin reserved by orders that are submitted but not yet filled
// is the broker's to know, since it holds the orders; it passes that to size.
//
// TODO: the margin is in the quote currency, assumed to be the account
// currency (USD); see moneyScale.
func (act *account) usedMargin() money {
	var total money
	for _, pos := range act.positions {
		total += marginFor(pos.instrument, pos.units, pos.markPrice)
	}
	return total
}

// marginFor is the margin units of symbol need at price p, rounded up to the
// cent: units * p * marginRate. Sizing refuses an unknown instrument, so only a
// bug can reach here with one.
func marginFor(symbol string, units int64, p price) money {
	inst, ok := instruments[symbol]
	if !ok {
		panic(fmt.Sprintf("margin for %q: %v", symbol, errUnknownInstrument))
	}
	return money(muldiv(
		[]int64{units, int64(p), int64(inst.marginRate), moneyScale},
		[]int64{priceScale, rateScale},
		true))
}

// maxPrice and maxOrderUnits bound what reaches the money math: bars and orders
// above maxPrice are refused and sizing caps units at maxOrderUnits. One position's
// P/L or margin then stays within $10 trillion. Aggregate balances and totals
// use ordinary int64 cents; runs approaching its roughly $92 quadrillion limit
// are outside this prototype's intended range.
const (
	maxPrice      price = 1_000_000_000 // 10,000.00000
	maxOrderUnits int64 = 1_000_000_000
)

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
//   - open: sized to the smaller of the risk limit (a stop-out at the stop loses
//     about riskPct of equity; it is planned risk, not a cap) and the margin limit
//     (see sizeOpen), or rejected with errInsufficientMargin or errOrderTooSmall
//     if that is below the instrument's minimum.
//   - close: units stay 0 until the fill, which closes the whole position.
//   - add, reduce and anything else: errUnsupportedAction until their size
//     rules are defined.
func (act *account) size(o *order, reserved money) error {
	inst, ok := instruments[o.instrument]
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
	o.record(stateSized, o.Time, "")
	return nil
}

// sizeOpen sets o.units for an opening order. order.validate has already checked
// its prices. Two limits apply, and the smaller one wins:
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
// The smaller limit is then capped at maxOrderUnits and the instrument's maximum
// and rounded down to its step. Only if the result is below the instrument's
// minimum is the order rejected: with errInsufficientMargin when margin was the
// limit, and with errOrderTooSmall when the risk budget was.
func (act *account) sizeOpen(o *order, inst *instrument, reserved money) error {
	if inst.marginRate <= 0 {
		return fmt.Errorf("size open: %w: %s has no margin rate", errInstrumentConfig, inst.symbol)
	}

	equity := act.equity()
	margin := equity - act.usedMargin() - reserved
	if margin <= 0 {
		return reject(o, o.Time, "%w", errInsufficientMargin)
	}

	dist := max(int64(o.price-o.stopLoss), int64(o.stopLoss-o.price))
	riskUnits := muldiv(
		[]int64{int64(equity), int64(act.riskPct), priceScale},
		[]int64{rateScale, dist, moneyScale},
		false)
	marginUnits := muldiv(
		[]int64{int64(margin), priceScale, rateScale},
		[]int64{int64(o.price), int64(inst.marginRate), moneyScale},
		false)

	units, marginLimited := min(riskUnits, marginUnits, maxOrderUnits), marginUnits < riskUnits
	if inst.maxUnits > 0 {
		units = min(units, inst.maxUnits)
	}
	if inst.unitStep > 1 {
		units -= units % inst.unitStep
	}

	if least := max(inst.minUnits, 1); units < least {
		if marginLimited {
			return reject(o, o.Time, "%w: margin covers %d units, minimum %d",
				errInsufficientMargin, units, least)
		}
		return reject(o, o.Time, "%w: %d units, minimum %d", errOrderTooSmall, units, least)
	}

	o.units = units
	return nil
}

// muldiv returns the product of nums divided by the product of dens, rounded
// down or, with roundUp, up. It multiplies everything before dividing, in
// arbitrary precision, so there is a single rounding. A result too large for an
// int64 saturates: only a sizing limit can get that large, and sizing then caps it.
func muldiv(nums, dens []int64, roundUp bool) int64 {
	n, d := big.NewInt(1), big.NewInt(1)
	for _, v := range nums {
		n.Mul(n, big.NewInt(v))
	}
	for _, v := range dens {
		d.Mul(d, big.NewInt(v))
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
	switch {
	case q.IsInt64():
		return q.Int64()
	case q.Sign() > 0:
		return math.MaxInt64
	}
	return math.MinInt64
}

// mark values the open position in b's instrument at the bar's close. The
// broker calls it after the bar's fills, so a position opened at the bar's open
// is valued at its close.
func (a *account) mark(b bar) {
	if pos := a.positions[b.instrument]; pos != nil {
		pos.markPrice = b.c
	}
}

// applyFill applies a fill at price p, at time at, to the account: it validates
// the fill, realizes any profit or loss, then updates the balance and the position
// together, so a failed fill leaves the account as it was. One position per
// instrument (netting): an open creates it, a close removes it and the order takes
// the closed size. Add and reduce wait on the size type.
//
// The broker only reports the execution; it never touches the balance.
func (a *account) applyFill(o *order, p price, at time.Time) error {
	pos := a.positions[o.instrument]
	switch o.action {
	case open:
		if pos != nil {
			return fmt.Errorf("%w: %s is already %s", errPositionExists, o.instrument, pos.direction)
		}
		// Sizing checked the margin when the order was submitted, but a resting
		// order fills later, after equity may have fallen. The instruction is
		// never resized: it fills as submitted or is rejected.
		need, avail := marginFor(o.instrument, o.units, p), a.marginAvailable()
		if need > avail {
			return rejected("%w: %s needs %d cents of margin at the fill, %d available",
				errInsufficientMargin, o.instrument, need, avail)
		}
		a.positions[o.instrument] = &position{
			instrument: o.instrument,
			direction:  o.direction,
			units:      o.units,
			entryPrice: p,
			entryTime:  at,
			markPrice:  p,
		}
	case close:
		if pos == nil {
			return rejected("%w: %s", errNoPosition, o.instrument)
		}
		pnl := pos.pnlAt(p) // realized: closing at the fill
		a.balance += pnl
		o.units = pos.units
		a.trades = append(a.trades, trade{
			instrument: pos.instrument,
			direction:  pos.direction,
			units:      pos.units,
			entryPrice: pos.entryPrice,
			entryTime:  pos.entryTime,
			exitPrice:  p,
			exitTime:   at,
			reason:     o.reason,
			pnl:        pnl,
		})
		delete(a.positions, o.instrument)
	default:
		return fmt.Errorf("fill %s: %w", o.action, errUnsupportedAction)
	}
	return nil
}

// unrealizedPnL is the profit or loss, in cents, of the position at its latest
// mark.
func (p *position) unrealizedPnL() money {
	return p.pnlAt(p.markPrice)
}

// pnlAt is the profit or loss, in cents, of the position valued at exit:
// realized when exit is a fill, unrealized when it is a mark. It rounds down
// (toward a larger loss) once, to the cent.
//
// TODO: this assumes the quote currency is the account currency (USD) and
// excludes spread and commission; see moneyScale.
func (p *position) pnlAt(exit price) money {
	delta := int64(exit - p.entryPrice)
	if p.direction == short {
		delta = -delta
	}
	return money(muldiv([]int64{delta, p.units, moneyScale}, []int64{priceScale}, false))
}
