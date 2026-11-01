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
	errAccountConflict   = errors.New("order account conflicts with the trader's account")
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

	o.timestamp = b.Time
	o.record(stateRequested, "")
	if err := tr.handleOrder(ctx, o); err != nil {
		return fmt.Errorf("handling %s %s at %s %s: %w",
			o.action, o.instrument, b.instrument, b.Time.Format(time.DateOnly), err)
	}
	if o.state == stateRejected {
		// Expected trading outcome: already recorded, so log it and keep going.
		slog.Warn("order rejected",
			"reason", o.history[len(o.history)-1].reason,
			"time", b.Time,
			"instrument", o.instrument,
			"action", o.action,
			"direction", o.direction)
	}
	return nil
}

// handleOrder sizes the order and submits it to the broker. An expected
// rejection is recorded on the order and is not an error; anything else is
// returned and stops the run.
func (tr *trader) handleOrder(ctx context.Context, o *order) error {
	// The strategy knows nothing of accounts; the trader says whose order this
	// is. An account already on the order is never overwritten.
	switch o.accountID {
	case "":
		o.accountID = tr.account.id
	case tr.account.id:
	default:
		return fmt.Errorf("%w: order is for account %s, trader has %s",
			errAccountConflict, o.accountID, tr.account.id)
	}

	err := tr.account.size(o)
	if err == nil {
		err = tr.broker.submit(ctx, o)
	}
	if isRejection(err) {
		if o.state != stateRejected { // the rejecting step normally records it
			o.record(stateRejected, err.Error())
		}
		return nil
	}
	return err
}
