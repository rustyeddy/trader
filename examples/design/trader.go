package main

import (
	"context"
	"errors"
)

// errInsufficientMargin is an expected trading outcome, not a runtime
// failure: the order is rejected and the run continues.
var errInsufficientMargin = errors.New("insufficient margin available")

var (
	errUnknownBroker  = errors.New("unknown broker")
	errUnknownAccount = errors.New("unknown account")
)

type trader struct {
	broker  *broker
	account *account
}

func (tr *trader) handleOrder(ctx context.Context, o *order) (err error) {
	switch o.action {
	case open:
		if tr.account.marginAvailable() <= 0 {
			return errInsufficientMargin
		}
		err = tr.openTrade(o)

	case close:
		err = tr.closeTrade(o)
	}
	return err
}

func (tr *trader) openTrade(o *order) error {

	return nil
}

func (tr *trader) closeTrade(o *order) error {

	return nil
}
