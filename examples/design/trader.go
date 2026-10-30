package main

import (
	"context"
	"errors"
)

// errInsufficientMargin is an expected trading outcome, not a runtime
// failure: the intent is rejected and the run continues.
var errInsufficientMargin = errors.New("insufficient margin available")

var (
	errUnknownBroker  = errors.New("unknown broker")
	errUnknownAccount = errors.New("unknown account")
)

type trader struct {
	broker  *broker
	account *account
}

func (tr *trader) handleIntent(ctx context.Context, i intent) (err error) {
	switch i.action {
	case open:
		if tr.account.marginAvailable() <= 0 {
			return errInsufficientMargin
		}
		err = tr.openTrade(i)

	case close:
		err = tr.closeTrade(i)
	}
	return err
}

func (tr *trader) openTrade(i intent) error {

	return nil
}

func (tr *trader) closeTrade(i intent) error {

	return nil
}
