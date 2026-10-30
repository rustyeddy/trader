package main

import (
	"context"
	"errors"
)

type trader struct {
	broker  *broker
	account *account
}

func (tr *trader) handleIntent(ctx context.Context, i intent) (err error) {
	switch i.action {
	case open:
		if tr.account.marginAvailable() <= 0 {
			return errors.New("Not enough margin available")
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
