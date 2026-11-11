package order

import "fmt"

// TimeInForce controls how long an order remains eligible for execution.
//
// GTD (good-til-date) is deliberately not included: it needs an
// expiration field this package's initial scope does not call for.
// Adding it later is additive, not a breaking change.
type TimeInForce uint8

const (
	tifUnset TimeInForce = iota
	// GTC (good-til-canceled) remains working until filled or
	// explicitly canceled.
	GTC
	// DAY remains working only for the trading session it was
	// submitted in.
	DAY
	// IOC (immediate-or-cancel) fills whatever quantity it can
	// immediately and cancels the remainder.
	IOC
	// FOK (fill-or-kill) fills its entire quantity immediately or is
	// canceled in full.
	FOK
)

// String returns a human-readable TimeInForce name.
func (f TimeInForce) String() string {
	switch f {
	case GTC:
		return "gtc"
	case DAY:
		return "day"
	case IOC:
		return "ioc"
	case FOK:
		return "fok"
	default:
		return fmt.Sprintf("TimeInForce(%d)", uint8(f))
	}
}

func (f TimeInForce) valid() bool {
	switch f {
	case GTC, DAY, IOC, FOK:
		return true
	default:
		return false
	}
}
