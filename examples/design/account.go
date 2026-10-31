package main

import (
	"errors"
	"fmt"
)

type money int64

type account struct {
	id      string
	balance money
}

func newAccount(id string, balance money) *account {
	return &account{
		id:      id,
		balance: balance,
	}
}

func (act *account) marginAvailable() money {
	return act.balance
}

// defaultOrderUnits is the placeholder size of every opening order until a
// real sizing policy exists (see size).
const defaultOrderUnits int64 = 1000

// errUnsupportedAction is returned for actions the account cannot size yet.
// It is a programming gap, not a trading outcome.
var errUnsupportedAction = errors.New("action not supported yet")

// size decides how many units the order trades, or rejects it. The strategy
// says what it wants; the account decides how much.
//
//   - open: rejected with errInsufficientMargin when no margin is available,
//     otherwise sized at defaultOrderUnits.
//   - close: units stay 0, meaning the whole position, which is resolved once
//     positions exist.
//   - add, reduce and anything else: errUnsupportedAction until their size
//     rules are defined.
func (act *account) size(o *order) error {
	switch o.action {
	case open:
		if act.marginAvailable() <= 0 {
			return errInsufficientMargin
		}
		o.units = defaultOrderUnits
	case close:
		// whole position
	default:
		return fmt.Errorf("size %s: %w", o.action, errUnsupportedAction)
	}
	o.state = stateSized
	return nil
}
