package trader

import (
	"sort"

	"github.com/rustyeddy/trader/num"
)

// marginStatus is how an account stands against its broker's margin rules.
type marginStatus uint8

const (
	marginHealthy   marginStatus = iota // enough margin
	marginWarning                       // running low: warn, but nothing is closed
	marginLiquidate                     // the broker must close positions
)

func (s marginStatus) String() string {
	switch s {
	case marginHealthy:
		return "healthy"
	case marginWarning:
		return "warning"
	case marginLiquidate:
		return "liquidate"
	}
	return "unknown"
}

// marginPolicy answers: is the account healthy, in warning, or subject to
// liquidation? It only reads the account.
type marginPolicy func(*account) (marginStatus, error)

// closeoutPolicy answers: which position should be liquidated next? It returns
// nil when the account has no position to close. It only reads the account.
type closeoutPolicy func(*account) (*position, error)

// marginLevelPolicy judges an account by its margin level, equity as a
// fraction of the margin in use: below closeout it must be liquidated, below
// warn it is in warning. An account that uses no margin is healthy.
func marginLevelPolicy(warn, closeout num.Rate) marginPolicy {
	return func(a *account) (marginStatus, error) {
		eq, err := a.equity()
		if err != nil {
			return marginHealthy, err
		}
		used, err := a.usedMargin()
		if err != nil {
			return marginHealthy, err
		}
		if sign, err := moneySign(used); err != nil {
			return marginHealthy, err
		} else if sign <= 0 {
			return marginHealthy, nil
		}
		below, err := levelBelow(eq, used, closeout)
		if err != nil {
			return marginHealthy, err
		}
		if below {
			return marginLiquidate, nil
		}
		if below, err = levelBelow(eq, used, warn); err != nil {
			return marginHealthy, err
		}
		if below {
			return marginWarning, nil
		}
		return marginHealthy, nil
	}
}

// levelBelow reports whether the margin level equity/used is strictly below
// level; equality is healthy. The level is a Rate rounded at 1e-8, so the
// equity at which the answer flips is within 0.5e-8 x used of the exact
// boundary (ADR-087, "Margin closeout boundary rule"). used must be positive.
func levelBelow(equity, used num.Money, level num.Rate) (bool, error) {
	got, err := equity.Div(used)
	if err != nil {
		return false, err
	}
	return got.Cmp(level) < 0, nil
}

// defaultMarginPolicy warns when equity falls below the margin in use (a
// margin level of 100%) and liquidates below half of it, as a retail forex
// broker with a 50% closeout level does.
var defaultMarginPolicy = marginLevelPolicy(rateOne, rateHalf)

// closeWorstLoser picks the position with the largest unrealized loss. Ties go
// to the lowest instrument name so a run is replayable.
func closeWorstLoser(a *account) (*position, error) {
	var worst *position
	var worstPnL num.Money
	for _, sym := range a.positionSymbols() {
		pos := a.positions[sym]
		pnl, err := pos.unrealizedPnL(a.currency)
		if err != nil {
			return nil, err
		}
		if worst == nil {
			worst, worstPnL = pos, pnl
			continue
		}
		less, err := pnl.Cmp(worstPnL)
		if err != nil {
			return nil, err
		}
		if less < 0 {
			worst, worstPnL = pos, pnl
		}
	}
	return worst, nil
}

// closeLargestMargin picks the position that uses the most margin, which frees
// the most per closeout. Ties go to the lowest instrument name.
func closeLargestMargin(a *account) (*position, error) {
	var largest *position
	var largestMargin num.Money
	for _, sym := range a.positionSymbols() {
		pos := a.positions[sym]
		m, err := marginFor(pos.instrument, pos.units, pos.markPrice, a.currency)
		if err != nil {
			return nil, err
		}
		if largest == nil {
			largest, largestMargin = pos, m
			continue
		}
		more, err := m.Cmp(largestMargin)
		if err != nil {
			return nil, err
		}
		if more > 0 {
			largest, largestMargin = pos, m
		}
	}
	return largest, nil
}

// positionSymbols lists the instruments with an open position in a fixed
// order, since map order would make closeout choices differ between runs.
func (a *account) positionSymbols() []string {
	syms := make([]string, 0, len(a.positions))
	for sym := range a.positions {
		syms = append(syms, sym)
	}
	sort.Strings(syms)
	return syms
}
