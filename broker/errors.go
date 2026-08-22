package broker

import "errors"

var (
	// ErrAccountNotFound reports that Broker.OpenAccount was called
	// with an account id the broker session does not recognize. It is
	// distinct from ErrOrderNotFound: this always means the *account*
	// handle itself could not be opened, never that an order within an
	// already-open account is unrecognized.
	ErrAccountNotFound = errors.New("broker: account not found")

	// ErrOrderNotFound reports that Account.Cancel or Account.Replace
	// was called with an order id the account does not recognize —
	// distinct from ErrAccountNotFound, which means the account handle
	// itself could not be opened.
	ErrOrderNotFound = errors.New("broker: order not found")

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
