package trader

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// errInsufficientMargin is an expected trading outcome, not a runtime
// failure: the order is rejected and the run continues.
var errInsufficientMargin = errors.New("insufficient margin available")

// errOrderTooLarge is an expected outcome too: at the price a resting order
// filled at, its notional is above the account's limit. The order is never
// resized: it fills as submitted or is rejected.
var errOrderTooLarge = errors.New("order notional above the account limit")

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

// errLoosensStop is an expected outcome: an amend would loosen the protection
// the account sized the position from. Only tightening is allowed.
var errLoosensStop = errors.New("amend does not tighten")

// errStopWrongSide is an expected outcome: an amended stop or target is not on
// its side of the latest price, so it would trigger at once or is meaningless.
var errStopWrongSide = errors.New("amend is on the wrong side of the price")

// errNoDecisionPrice is an expected outcome: a market open names an instrument
// with no bar in the frame it was decided on, so there is no close to size from.
var errNoDecisionPrice = errors.New("no price to size from")

// errStopBeyondClose is an expected outcome: a market open's stop is not on the
// losing side of its decision close, so there is no risk distance to size from.
var errStopBeyondClose = errors.New("stop is not on the losing side of the close")

// errNoPosition is also an expected outcome: a close arrives when there is no
// position, because its open was rejected or a stop has already closed it.
var errNoPosition = errors.New("no position to close")

// errPositionExists is a fatal error: netting allows one position per
// instrument, and add is not supported yet.
var errPositionExists = errors.New("position already exists")

var (
	errUnknownAccount    = errors.New("unknown account")
	errAccountConflict   = errors.New("order account conflicts with the trader's account")
	errUnknownInstrument = errors.New("unknown instrument")
)

type trader struct {
	broker   *broker
	account  *account
	strategy strategy

	// rejected keeps the orders refused before they reached the broker's queue,
	// in the order they were refused. They never join broker.completed, which
	// keeps the rejections that happen at the fill, so no order is in both.
	rejected []*order

	// accountLog, when not nil, observes the account around every frame.
	accountLog *accountLog

	// feedbackLog is what a strategy that wants feedback has been told.
	feedbackLog feedbackLog
}

// onBar handles one bar: onFrame with a frame of that bar alone.
func (tr *trader) onBar(ctx context.Context, b marketdata.Bar) error {
	return tr.onFrame(ctx, frame{availableAt: b.AvailableAt, bars: []marketdata.Bar{b}})
}

// onFrame advances the broker (fills and marks) through the whole frame once,
// then asks the strategy once, with the whole frame, what it wants. The trader
// owns the order lifecycle from there: it stamps when each order was decided (the
// frame's availability), validates it, records it as requested, has the broker
// size and queue it, and records an expected rejection without stopping the run.
// Orders are submitted in the order the strategy returned them, after it has seen
// the whole frame, so when two compete for margin the first is sized first and the
// second sees the margin it reserved.
func (tr *trader) onFrame(ctx context.Context, f frame) error {
	if tr.accountLog != nil && tr.accountLog.seq == 0 && len(f.bars) > 0 {
		// Before anything trades: the opening of the first traded frame.
		if err := tr.accountLog.observe(observeInitial, f.bars[0].Time, tr.account, nil); err != nil {
			return err
		}
	}
	if err := tr.broker.onFrame(ctx, f); err != nil {
		if len(f.bars) == 1 {
			b := f.bars[0]
			return fmt.Errorf("broker on bar %s %s: %w", b.Instrument, b.Time.Format(time.DateOnly), err)
		}
		return fmt.Errorf("broker on frame at %s: %w", f.availableAt.Format(time.RFC3339), err)
	}

	if fb, ok := tr.strategy.(feedbacker); ok {
		fb.feedback(tr.collectEvents())
	}
	orders, err := tr.strategy.onBars(ctx, f.bars)
	if err != nil {
		return fmt.Errorf("strategy %s: %w", tr.strategy.name(), err)
	}
	for _, o := range orders {
		if o == nil {
			continue
		}
		o.decidedAt = f.availableAt
		o.decisionClose = closeOf(f.bars, o.instrument)
		if err := tr.handleOrder(ctx, o); err != nil {
			return err
		}
	}
	if tr.accountLog != nil {
		if err := tr.accountLog.observe(observeFrame, f.availableAt, tr.account, staleInstruments(tr.account, f)); err != nil {
			return fmt.Errorf("frame at %s: %w", f.availableAt.Format(time.RFC3339), err)
		}
	}
	// The journal could not write: stop at this safe point, after the frame is
	// whole, rather than trade on without the record the run is meant to keep.
	if err := tr.broker.journal.err(); err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	return nil
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

	tr.watch(o)
	o.journal = tr.broker.journal // before the first record, so the requested line is kept
	o.record(stateRequested, o.Time, "")
	err := tr.priceAtDecision(o)
	if err == nil {
		err = tr.broker.submit(ctx, o)
	}
	if err != nil {
		if !isRejection(err) {
			return fmt.Errorf("handling %s %s at %s: %w",
				o.action, o.instrument, o.Time.Format(time.DateOnly), err)
		}
		tr.rejected = append(tr.rejected, o)
	}
	return nil
}

// closeOf is the close of the instrument's bar among bars, or zero when none of
// them is the instrument's.
func closeOf(bars []marketdata.Bar, instrument string) num.Price {
	for _, b := range bars {
		if b.Instrument == instrument {
			return b.OHLC().Close
		}
	}
	return num.Price{}
}

// priceAtDecision makes a market open's reference price the close of the bar it
// was decided on, so that the account sizes it, and reserves its margin, from a
// price Trader knows. The strategy's own price is only what it asked: it stays
// on the requested journal line and cannot change the size. The order is
// rejected when the frame had no bar of its instrument, or when its stop is not
// on the losing side of the close.
//
// Other orders keep their price: a limit or stop entry's price is its trigger.
// An order the trader did not decide on a frame has no decision close and is
// left alone, as is one for an unknown instrument, which sizing refuses as an
// error.
func (tr *trader) priceAtDecision(o *order) error {
	if o.action != open || o.orderType != marketOrder || o.decidedAt.IsZero() {
		return nil
	}
	if _, ok := tr.account.markets.lookup(o.instrument); !ok {
		return nil
	}
	if o.decisionClose.IsZero() {
		return reject(o, o.Time, "%w: no %s bar at %s", errNoDecisionPrice, o.instrument, o.decidedAt.Format(time.RFC3339))
	}
	if o.price.Equal(o.decisionClose) {
		return nil
	}
	o.claimedPrice, o.price = o.price, o.decisionClose
	if !o.beyond(o.stopLoss, true) {
		return reject(o, o.Time, "%w: %s stop %s, close %s",
			errStopBeyondClose, o.direction, o.stopLoss, o.price)
	}
	return nil
}
