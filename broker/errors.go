package broker

import "errors"

var (
	// ErrAccountNotFound reports that Broker.OpenAccount was called
	// with an account id the broker session does not recognize.
	ErrAccountNotFound = errors.New("broker: account not found")

	// ErrUnsupported reports that a broker adapter does not support
	// requested behavior — for example an order.Type or
	// order.TimeInForce value the underlying venue does not accept.
	// Trader's order vocabulary is closed (see order.Side, order.Type,
	// order.TimeInForce), so this always means "this adapter or venue
	// does not support it," never "Trader does not know this concept."
	ErrUnsupported = errors.New("broker: unsupported")

	// ErrClosed reports that a Broker or Account method was called
	// after the owning Broker's Close returned.
	ErrClosed = errors.New("broker: closed")
)
