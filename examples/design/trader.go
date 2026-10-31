package main

import (
	"context"
	"errors"
)

// errInsufficientMargin is an expected trading outcome, not a runtime
// failure: the order is rejected and the run continues.
var errInsufficientMargin = errors.New("insufficient margin available")

// errOrderTooSmall is also an expected outcome: the risk budget, or the
// instrument's limits, leave nothing worth trading.
var errOrderTooSmall = errors.New("order size below the minimum")

// isRejection reports whether err is an expected trading outcome that rejects
// the order and lets the run continue, as opposed to a failure that stops it.
func isRejection(err error) bool {
	return errors.Is(err, errInsufficientMargin) || errors.Is(err, errOrderTooSmall)
}

var (
	errUnknownBroker     = errors.New("unknown broker")
	errUnknownAccount    = errors.New("unknown account")
	errUnknownInstrument = errors.New("unknown instrument")
	errInstrumentConfig  = errors.New("instrument not configured")
)

type trader struct {
	broker  *broker
	account *account
}

func (tr *trader) handleOrder(ctx context.Context, o *order) (err error) {
	// The strategy knows nothing of accounts; the trader says whose order this is.
	o.accountID = tr.account.id

	if err := tr.account.size(o); err != nil {
		return err
	}

	return tr.broker.submit(ctx, o)
}
