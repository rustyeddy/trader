package main

import (
	"fmt"
	"math/big"
	"sort"
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
func marginLevelPolicy(warn, closeout rate) marginPolicy {
	return func(a *account) (marginStatus, error) {
		eq, err := a.equity()
		if err != nil {
			return marginHealthy, err
		}
		used, err := a.usedMargin()
		if err != nil {
			return marginHealthy, err
		}
		switch {
		case used <= 0:
			return marginHealthy, nil
		case levelBelow(eq, used, closeout):
			return marginLiquidate, nil
		case levelBelow(eq, used, warn):
			return marginWarning, nil
		}
		return marginHealthy, nil
	}
}

// levelBelow reports whether equity/used < level, exactly: equity * rateScale
// < level * used, in arbitrary precision.
func levelBelow(equity, used money, level rate) bool {
	l := new(big.Int).Mul(big.NewInt(int64(equity)), big.NewInt(rateScale))
	r := new(big.Int).Mul(big.NewInt(int64(level)), big.NewInt(int64(used)))
	return l.Cmp(r) < 0
}

// defaultMarginPolicy warns when equity falls below the margin in use (a
// margin level of 100%) and liquidates below half of it, as a retail forex
// broker with a 50% closeout level does.
var defaultMarginPolicy = marginLevelPolicy(rate(rateScale), rate(rateScale/2))

// closeWorstLoser picks the position with the largest unrealized loss. Ties go
// to the lowest instrument name so a run is replayable.
func closeWorstLoser(a *account) (*position, error) {
	var worst *position
	var worstPnL money
	for _, sym := range a.positionSymbols() {
		pos := a.positions[sym]
		pnl, err := pos.unrealizedPnL()
		if err != nil {
			return nil, fmt.Errorf("closeout %s: %w", sym, err)
		}
		if worst == nil || pnl < worstPnL {
			worst, worstPnL = pos, pnl
		}
	}
	return worst, nil
}

// closeLargestMargin picks the position that uses the most margin, which frees
// the most per closeout. Ties go to the lowest instrument name.
func closeLargestMargin(a *account) (*position, error) {
	var largest *position
	var largestMargin money
	for _, sym := range a.positionSymbols() {
		pos := a.positions[sym]
		m, err := marginFor(pos.instrument, pos.units, pos.markPrice)
		if err != nil {
			return nil, fmt.Errorf("closeout %s: %w", sym, err)
		}
		if largest == nil || m > largestMargin {
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
