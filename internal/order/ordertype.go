package order

import "fmt"

// Type is an order's execution type.
type Type uint8

const (
	typeUnset Type = iota
	// Market executes immediately at the best available price; it
	// carries no limit or stop price.
	Market
	// Limit executes at LimitPrice or better; it requires a limit price
	// and carries no stop price.
	Limit
	// Stop becomes a market order once StopPrice trades; it requires a
	// stop price and carries no limit price.
	Stop
	// StopLimit becomes a limit order once StopPrice trades; it
	// requires both a stop price and a limit price.
	StopLimit
)

// String returns a human-readable Type name.
func (t Type) String() string {
	switch t {
	case Market:
		return "market"
	case Limit:
		return "limit"
	case Stop:
		return "stop"
	case StopLimit:
		return "stop_limit"
	default:
		return fmt.Sprintf("Type(%d)", uint8(t))
	}
}

func (t Type) valid() bool {
	switch t {
	case Market, Limit, Stop, StopLimit:
		return true
	default:
		return false
	}
}

// requiresLimitPrice reports whether t requires a non-nil limit price.
func (t Type) requiresLimitPrice() bool {
	return t == Limit || t == StopLimit
}

// requiresStopPrice reports whether t requires a non-nil stop price.
func (t Type) requiresStopPrice() bool {
	return t == Stop || t == StopLimit
}
