package trader

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/rustyeddy/trader/num"
)

// direction is the direction of a position, not an order side (buy/sell).
type direction uint8

const (
	noDirection direction = iota
	long
	short
)

// action is what the order asks to do to the position in its instrument.
type action uint8

const (
	noAction action = iota
	open
	add
	close
	reduce
)

type orderType uint8

const (
	noOrder orderType = iota
	marketOrder
	limitOrder
	stopOrder
)

// orderState is where an order is in its lifecycle: requested by a strategy,
// sized by the account, submitted to the broker, then filled or rejected. A
// rejection declines an order; a cancellation ends one that was already
// accepted (submitted) before it filled.
type orderState uint8

const (
	stateRequested orderState = iota
	stateSized
	stateSubmitted
	stateFilled
	stateRejected
	stateCancelled // accepted earlier, no longer active
)

// orderReason is why the order exists. A liquidation is still an ordinary
// closing order that ends filled; the reason is what tells it apart in the
// audit trail.
type orderReason uint8

const (
	reasonStrategy       orderReason = iota // the strategy asked for it
	reasonStopLoss                          // a stop-loss triggered
	reasonMarginCloseout                    // the broker liquidated the position
	reasonTakeProfit                        // a take-profit limit triggered
)

func (r orderReason) String() string {
	switch r {
	case reasonStrategy:
		return "strategy"
	case reasonStopLoss:
		return "stop loss"
	case reasonMarginCloseout:
		return "margin closeout"
	case reasonTakeProfit:
		return "take profit"
	}
	return "unknown"
}

// orderEvent is one entry in an order's history.
type orderEvent struct {
	state  orderState
	time   time.Time
	reason string // why, for rejections
}

// order carries a request from the strategy through sizing, the broker and
// back to the trader. A strategy fills in what it wants (instrument, action,
// direction); later stages fill in units and prices. A nil *order from a
// strategy means there is nothing to do on that bar.
type order struct {
	id         string
	accountID  string // set by the trader before the order is sized and submitted
	instrument string
	direction  direction
	action     action
	orderType  orderType
	reason     orderReason // zero is reasonStrategy

	price      num.Price
	fillPrice  num.Price
	units      num.Quantity
	stopLoss   num.Price
	takeProfit num.Price // optional target on an open; 0 means none

	// trail makes the protective stop trail the market by this price distance,
	// ratcheting toward profit and never back. 0 is a fixed stop.
	// On the broker's stop order it is the distance the stop keeps.
	trail num.Price

	// expires is the first bar time at which a waiting entry is no longer live:
	// the broker cancels it instead of executing it on that bar or any later
	// one. The zero time means good until cancelled.
	expires time.Time

	// decidedAt is when the strategy decided on the order: the availability of
	// the bar it saw, stamped by the trader. The order may not fill on a bar
	// that opened before then, whatever the instrument. Zero only on orders
	// made outside the trader, such as the broker's own exits.
	decidedAt time.Time

	// Time is the simulated time of the step the order is on: the bar being
	// processed. The strategy sets it on a new order and record moves it, so it
	// is when the order last changed. The managed clock for live trading will
	// supply the times the same way.
	Time time.Time

	state   orderState
	history []orderEvent
}

// record moves the order to state s at time at, appends the event to its
// history and logs the change. Every step an order takes goes through here.
func (o *order) record(s orderState, at time.Time, reason string) {
	from := o.state
	first := len(o.history) == 0
	o.state, o.Time = s, at
	o.history = append(o.history, orderEvent{state: s, time: at, reason: reason})

	attrs := []any{
		"order", o.id,
		"instrument", o.instrument,
		"action", o.action,
	}
	if !first {
		attrs = append(attrs, "from", from)
	}
	attrs = append(attrs, "to", s)
	if o.reason != reasonStrategy {
		attrs = append(attrs, "cause", o.reason)
	}
	if reason != "" {
		attrs = append(attrs, "reason", reason)
	}
	slog.Info("order state", attrs...)
}

// reject records an expected rejection on the order at time at, logs it and
// returns it. The format should wrap the sentinel that names the rejection. It is
// the one place a rejection is recorded, so every refusal looks the same in the
// history and the log, whoever made it.
func reject(o *order, at time.Time, format string, args ...any) error {
	err := rejected(format, args...)
	o.record(stateRejected, at, err.Error())
	slog.Warn("order rejected",
		"reason", err,
		"time", o.Time,
		"instrument", o.instrument,
		"action", o.action,
		"direction", o.direction)
	return err
}

// errInvalidOrder marks an order whose fields are not a legal combination.
// It is a strategy bug, not a trading outcome.
var errInvalidOrder = errors.New("invalid order")

// validate checks that the order is a legal combination of fields. It is pure:
// stateful checks (such as a reduce larger than the position) belong to the
// handler.
//
// Every request must explicitly name marketOrder, limitOrder or stopOrder;
// noOrder and unknown types are invalid.
//
// An open must carry a reference price and a stop on the losing side of it, and
// any target on the winning side. Prices above maxPrice are refused here, so the
// money math cannot overflow.
//
// Add and reduce use marketOrder; their size rules wait on the size type being
// defined.
func (o *order) validate() error {
	if o.direction > short {
		return fmt.Errorf("%w: unknown direction %d", errInvalidOrder, o.direction)
	}
	if o.Time.IsZero() {
		return fmt.Errorf("%w: order requires the time of the bar it was made on", errInvalidOrder)
	}
	switch o.orderType {
	case marketOrder:
	case limitOrder, stopOrder:
		if o.action != open {
			return fmt.Errorf("%w: a %s order can only open (got %s)", errInvalidOrder, o.orderType, o.action)
		}
		if o.price.IsZero() {
			return fmt.Errorf("%w: a %s order requires a trigger price", errInvalidOrder, o.orderType)
		}
	default:
		return fmt.Errorf("%w: unsupported order type %d", errInvalidOrder, o.orderType)
	}
	if num.Max(o.price, o.stopLoss, o.takeProfit).Cmp(maxPrice) > 0 {
		return fmt.Errorf("%w: prices must not exceed %s", errInvalidOrder, maxPrice)
	}
	if !o.takeProfit.IsZero() && o.action != open {
		return fmt.Errorf("%w: take profit must be positive and only an open carries one", errInvalidOrder)
	}
	if !o.trail.IsZero() && o.action != open {
		return fmt.Errorf("%w: trail must be positive and only an open carries one", errInvalidOrder)
	}
	if !o.expires.IsZero() && (o.action != open || !o.expires.After(o.Time)) {
		return fmt.Errorf("%w: expiry must be after the order's time and only an open has one", errInvalidOrder)
	}
	switch o.action {
	case noAction:
		return fmt.Errorf("%w: order requires an action", errInvalidOrder)
	case open:
		if o.instrument == "" {
			return fmt.Errorf("%w: open requires an instrument", errInvalidOrder)
		}
		if o.direction == noDirection {
			return fmt.Errorf("%w: open requires a direction", errInvalidOrder)
		}
		// The strategy decides where the stop goes; the account only sizes
		// from it, so an open without one never reaches the account.
		if o.stopLoss.IsZero() {
			return fmt.Errorf("%w: open requires a stop loss set by the strategy", errInvalidOrder)
		}
		if o.price.IsZero() {
			return fmt.Errorf("%w: open requires a reference price", errInvalidOrder)
		}
		if !o.beyond(o.stopLoss, true) {
			return fmt.Errorf("%w: %s stop %s is not beyond the entry price %s",
				errInvalidOrder, o.direction, o.stopLoss, o.price)
		}
		if !o.takeProfit.IsZero() && !o.beyond(o.takeProfit, false) {
			return fmt.Errorf("%w: %s take profit %s is not beyond the entry price %s",
				errInvalidOrder, o.direction, o.takeProfit, o.price)
		}
	case add, reduce, close:
		if o.instrument == "" {
			return fmt.Errorf("%w: %s requires an instrument", errInvalidOrder, o.action)
		}
		if o.direction != noDirection {
			return fmt.Errorf("%w: %s takes no direction (got %s)", errInvalidOrder, o.action, o.direction)
		}
	default:
		return fmt.Errorf("%w: unknown action %d", errInvalidOrder, o.action)
	}
	return nil
}

// beyond reports whether p lies past the order's entry price on its losing
// side (where a stop belongs) or, with losing false, its winning side (where a
// target belongs). For a long the losing side is below the entry.
func (o *order) beyond(p num.Price, losing bool) bool {
	if (o.direction == long) == losing {
		return p.Cmp(o.price) < 0
	}
	return p.Cmp(o.price) > 0
}

func (d direction) String() string {
	switch d {
	case long:
		return "long"
	case short:
		return "short"
	}
	return "none"
}

func (a action) String() string {
	switch a {
	case noAction:
		return "none"
	case open:
		return "open"
	case add:
		return "add"
	case close:
		return "close"
	case reduce:
		return "reduce"
	}
	return "unknown"
}

func (s orderState) String() string {
	switch s {
	case stateRequested:
		return "requested"
	case stateSized:
		return "sized"
	case stateSubmitted:
		return "submitted"
	case stateFilled:
		return "filled"
	case stateRejected:
		return "rejected"
	case stateCancelled:
		return "cancelled"
	}
	return "unknown"
}

func (t orderType) String() string {
	switch t {
	case noOrder:
		return "none"
	case marketOrder:
		return "market"
	case limitOrder:
		return "limit"
	case stopOrder:
		return "stop"
	}
	return "unknown"
}
