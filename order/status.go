package order

import "fmt"

// Status is an Order's broker-reported lifecycle state. Unlike Side,
// Type, and TimeInForce — closed vocabulary Trader itself controls —
// Status reflects what a broker reports back, so it reserves its zero
// value for StatusUnknown rather than defaulting to something
// permissive: an adapter parsing an unrecognized broker status string
// produces StatusUnknown instead of guessing or crashing, and an
// uninitialized Status can never be silently mistaken for a working or
// filled order.
//
// This package defines Status's values only. Which transitions between
// them are legal — Working -> Filled, Canceled being terminal, and so
// on — is issue #29 (M1-11)'s responsibility, not this package's.
type Status uint8

const (
	// StatusUnknown is Status's zero value: an unrecognized or not-yet-
	// determined state.
	StatusUnknown Status = iota
	// StatusPendingSubmit means Trader has built the order but it has
	// not yet been acknowledged by a broker.
	StatusPendingSubmit
	// StatusWorking means the broker has accepted the order and it is
	// eligible for execution.
	StatusWorking
	// StatusPartiallyFilled means the broker has accepted the order and
	// executed part, but not all, of its accepted quantity.
	StatusPartiallyFilled
	// StatusFilled means the order's entire accepted quantity has
	// executed. This is a terminal state.
	StatusFilled
	// StatusPendingCancel means a cancel request has been submitted but
	// not yet confirmed by the broker.
	StatusPendingCancel
	// StatusCanceled means the broker confirmed the order was canceled
	// before it fully filled. This is a terminal state.
	StatusCanceled
	// StatusPendingReplace means a replace request has been submitted
	// but not yet confirmed by the broker.
	StatusPendingReplace
	// StatusRejected means the broker declined the order outright; see
	// Order.Rejection for why. This is a terminal state.
	StatusRejected
	// StatusExpired means the order's TimeInForce elapsed before it
	// fully filled. This is a terminal state.
	StatusExpired
)

// String returns a human-readable Status name.
func (s Status) String() string {
	switch s {
	case StatusUnknown:
		return "unknown"
	case StatusPendingSubmit:
		return "pending_submit"
	case StatusWorking:
		return "working"
	case StatusPartiallyFilled:
		return "partially_filled"
	case StatusFilled:
		return "filled"
	case StatusPendingCancel:
		return "pending_cancel"
	case StatusCanceled:
		return "canceled"
	case StatusPendingReplace:
		return "pending_replace"
	case StatusRejected:
		return "rejected"
	case StatusExpired:
		return "expired"
	default:
		return fmt.Sprintf("Status(%d)", uint8(s))
	}
}

// valid reports whether s is one of Status's defined values, including
// StatusUnknown: unlike Side/Type/TimeInForce's zero value, StatusUnknown
// is itself a legitimate, storable value for broker-reported state
// Trader does not recognize — valid rejects only out-of-range values
// such as Status(200).
func (s Status) valid() bool {
	switch s {
	case StatusUnknown, StatusPendingSubmit, StatusWorking, StatusPartiallyFilled,
		StatusFilled, StatusPendingCancel, StatusCanceled, StatusPendingReplace,
		StatusRejected, StatusExpired:
		return true
	default:
		return false
	}
}

// requiresAcceptance reports whether s implies the broker has accepted
// the order, and therefore that Order.AcceptedQuantity must be non-nil.
func (s Status) requiresAcceptance() bool {
	switch s {
	case StatusWorking, StatusPartiallyFilled, StatusFilled, StatusPendingCancel,
		StatusCanceled, StatusPendingReplace, StatusExpired:
		return true
	default:
		return false
	}
}

// precludesAcceptance reports whether s implies the broker has not (or
// has not yet) accepted the order, and therefore that
// Order.AcceptedQuantity must be nil.
func (s Status) precludesAcceptance() bool {
	switch s {
	case StatusPendingSubmit, StatusRejected:
		return true
	default:
		return false
	}
}
