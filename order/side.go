package order

import "fmt"

// Side is the transaction direction of an order or fill: Buy or Sell.
// Side is Trader-controlled, closed vocabulary — unlike Status or
// RejectReason, nothing external reports a Side Trader doesn't already
// know about, so there is no "unknown" sentinel; construction sites
// reject anything outside the two defined values.
type Side uint8

const (
	sideUnset Side = iota
	Buy
	Sell
)

// String returns a human-readable Side name.
func (s Side) String() string {
	switch s {
	case Buy:
		return "buy"
	case Sell:
		return "sell"
	default:
		return fmt.Sprintf("Side(%d)", uint8(s))
	}
}

func (s Side) valid() bool {
	return s == Buy || s == Sell
}
