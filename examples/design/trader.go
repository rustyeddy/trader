package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
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
	broker   *broker
	account  *account
	strategy strategy
}

// onBar advances the broker and account to the bar, then lets the strategy
// react to it. The trader owns the order lifecycle from here: it validates the
// strategy's order, records it as requested, sizes and submits it, and records
// an expected rejection without stopping the run.
func (tr *trader) onBar(ctx context.Context, b bar) error {
	if err := tr.broker.onBar(ctx, b); err != nil {
		return fmt.Errorf("broker on bar %s %s: %w", b.instrument, b.Time.Format(time.DateOnly), err)
	}
	if err := tr.account.onBar(ctx, b); err != nil {
		return fmt.Errorf("account on bar %s %s: %w", b.instrument, b.Time.Format(time.DateOnly), err)
	}

	o := tr.strategy.onBar(ctx, b)
	if o == nil {
		return nil // nothing to do on this bar
	}
	if err := o.validate(); err != nil {
		// A strategy bug, not a trading outcome: stop before the account.
		return fmt.Errorf("strategy order at %s %s: %w", b.instrument, b.Time.Format(time.DateOnly), err)
	}

	o.record(stateRequested, "")
	err := tr.handleOrder(ctx, o)

	switch {
	case isRejection(err):
		// Expected trading outcome: record it, log it and keep processing bars.
		if o.state != stateRejected { // the rejecting step normally records it
			o.record(stateRejected, err.Error())
		}
		slog.Warn("order rejected",
			"reason", err,
			"time", b.Time,
			"instrument", o.instrument,
			"action", o.action,
			"direction", o.direction)
	case err != nil:
		return fmt.Errorf("handling %s %s at %s %s: %w",
			o.action, o.instrument, b.instrument, b.Time.Format(time.DateOnly), err)
	}
	return nil
}

func (tr *trader) handleOrder(ctx context.Context, o *order) (err error) {
	// The strategy knows nothing of accounts; the trader says whose order this is.
	o.accountID = tr.account.id

	if err := tr.account.size(o); err != nil {
		return err
	}

	return tr.broker.submit(ctx, o)
}
