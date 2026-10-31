package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// orderHandler receives the orders a strategy requests.
type orderHandler interface {
	handleOrder(context.Context, *order) error
}

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
// sized by the account, submitted to the broker, then filled or rejected.
type orderState uint8

const (
	stateRequested orderState = iota
	stateSized
	stateSubmitted
	stateFilled
	stateRejected
)

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
	instrument string
	direction  direction
	action     action
	orderType  orderType

	price    price
	units    int64
	stopLoss price

	state   orderState
	history []orderEvent
}

// record moves the order to state s and appends the event to its history.
func (o *order) record(s orderState, t time.Time, reason string) {
	o.state = s
	o.history = append(o.history, orderEvent{state: s, time: t, reason: reason})
}

// errInvalidOrder marks an order whose fields are not a legal combination.
// It is a strategy bug, not a trading outcome.
var errInvalidOrder = errors.New("invalid order")

// validate checks that the order is a legal combination of fields. It is pure:
// stateful checks (such as a reduce larger than the position) belong to the
// handler.
//
// Add and reduce are only checked for instrument and direction; their size
// rules wait on the size type being defined.
func (o *order) validate() error {
	if o.direction > short {
		return fmt.Errorf("%w: unknown direction %d", errInvalidOrder, o.direction)
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
	}
	return "unknown"
}
