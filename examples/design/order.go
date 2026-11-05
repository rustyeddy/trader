package main

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
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

	price      price
	fillPrice  price
	units      int64
	stopLoss   price
	takeProfit price // optional target on an open; 0 means none

	// Time is the simulated time of the step the order is on: the bar
	// being processed. Whoever advances the order sets it (the trader when the
	// order is requested, the broker when it fills), and record stamps history
	// with it. The managed clock for live trading will set it the same way.
	Time time.Time

	state   orderState
	history []orderEvent
}

// record moves the order to state s, appends a timestamped event to its
// history (stamped with o.Time) and logs the change. Every step an order takes goes through here.
func (o *order) record(s orderState, reason string) {
	from := o.state
	first := len(o.history) == 0
	o.state = s
	o.history = append(o.history, orderEvent{state: s, time: o.Time, reason: reason})

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

// errInvalidOrder marks an order whose fields are not a legal combination.
// It is a strategy bug, not a trading outcome.
var errInvalidOrder = errors.New("invalid order")

// validate checks that the order is a legal combination of fields. It is pure:
// stateful checks (such as a reduce larger than the position) belong to the
// handler.
//
// An open must carry a positive stop price; whether it is on the losing side of
// the entry is checked when the order is sized.
//
// Add and reduce are only checked for instrument and direction; their size
// rules wait on the size type being defined.
func (o *order) validate() error {
	if o.direction > short {
		return fmt.Errorf("%w: unknown direction %d", errInvalidOrder, o.direction)
	}
	if o.Time.IsZero() {
		return fmt.Errorf("%w: order requires the time of the bar it was made on", errInvalidOrder)
	}
	if o.orderType == limitOrder || o.orderType == stopOrder {
		if o.action != open {
			return fmt.Errorf("%w: a %s order can only open (got %s)", errInvalidOrder, o.orderType, o.action)
		}
		if o.price <= 0 {
			return fmt.Errorf("%w: a %s order requires a trigger price", errInvalidOrder, o.orderType)
		}
	}
	if o.takeProfit < 0 || (o.takeProfit != 0 && o.action != open) {
		return fmt.Errorf("%w: take profit must be positive and only an open carries one", errInvalidOrder)
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
		if o.stopLoss <= 0 {
			return fmt.Errorf("%w: open requires a stop loss set by the strategy", errInvalidOrder)
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
