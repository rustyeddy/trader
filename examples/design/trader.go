package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// errInsufficientMargin is an expected trading outcome, not a runtime
// failure: the order is rejected and the run continues.
var errInsufficientMargin = errors.New("insufficient margin available")

// errOrderTooSmall is also an expected outcome: the risk budget, or the
// instrument's limits, leave nothing worth trading.
var errOrderTooSmall = errors.New("order size below the minimum")

// rejection marks an expected trading outcome: the order is refused and the
// run continues. Any error not wrapped in one is a failure that stops the run.
type rejection struct{ err error }

func (r rejection) Error() string { return r.err.Error() }
func (r rejection) Unwrap() error { return r.err }

// rejected makes an expected trading outcome; format should wrap the sentinel
// that names it, so callers can still test for it with errors.Is.
func rejected(format string, args ...any) error {
	return rejection{fmt.Errorf(format, args...)}
}

// isRejection reports whether err is an expected trading outcome that rejects
// the order and lets the run continue, as opposed to a failure that stops it.
func isRejection(err error) bool {
	var r rejection
	return errors.As(err, &r)
}

// errNoOrder is an expected outcome: a cancel names an order that is not
// waiting, usually because it already filled, expired or was cancelled.
var errNoOrder = errors.New("no such pending order")

// errOpenPending is an expected outcome: netting allows one position per
// instrument, so a second opening order is refused while one is still waiting
// for the account on that instrument.
var errOpenPending = errors.New("an open is already pending")

// errCloseoutGuard is an expected outcome: the account was margin-closed-out on
// this bar, so a strategy open on the same observation is refused.
var errCloseoutGuard = errors.New("margin closeout on this bar")

// errNoPosition is also an expected outcome: a close arrives for a position
// that never opened (its open was rejected), so there is nothing to close.
var errNoPosition = errors.New("no position to close")

// errPositionExists is a fatal error: netting allows one position per
// instrument, and add is not supported yet.
var errPositionExists = errors.New("position already exists")

var (
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

// onBar advances the broker (fills and marks) to the bar, then lets the
// strategy react to it. The trader owns the order lifecycle from here: it
// validates the strategy's order, records it as requested, has the broker size
// and queue it, and records an expected rejection without stopping the run.
func (tr *trader) onBar(ctx context.Context, b bar) error {
	if err := tr.broker.onBar(ctx, b); err != nil {
		return fmt.Errorf("broker on bar %s %s: %w", b.instrument, b.Time.Format(time.DateOnly), err)
	}

	o := tr.strategy.onBar(ctx, b)
	if o == nil {
		return nil
	}
	return tr.handleOrder(ctx, o)
}

// handleOrder submits the order to the broker, which has the account size it.
// An expected rejection has been recorded and logged by whoever rejected it
// and is not an error; anything else is returned and stops the run.
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

	if err := o.validate(); err != nil {
		// A strategy bug, not a trading outcome: stop before the account.
		return fmt.Errorf("strategy order at %s %s: %w", o.instrument, o.Time.Format(time.DateOnly), err)
	}

	o.record(stateRequested, "")
	if err := tr.broker.submit(ctx, o); err != nil && !isRejection(err) {
		return fmt.Errorf("handling %s %s at %s: %w",
			o.action, o.instrument, o.Time.Format(time.DateOnly), err)
	}
	return nil
}
