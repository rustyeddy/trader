package main

import (
	"context"
	"errors"
)

// errInsufficientMargin is an expected trading outcome, not a runtime
// failure: the order is rejected and the run continues.
var errInsufficientMargin = errors.New("insufficient margin available")

var (
	errUnknownBroker     = errors.New("unknown broker")
	errUnknownAccount    = errors.New("unknown account")
	errUnknownInstrument = errors.New("unknown instrument")
)

type trader struct {
	broker  *broker
	account *account
}

func (tr *trader) handleOrder(ctx context.Context, o *order) (err error) {

	if err := tr.account.size(o); err != nil {
		return err
	}

	return tr.broker.submit(ctx, tr.account.id, o)
}
