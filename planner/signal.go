package planner

import "github.com/rustyeddy/trader/market"

// Signal is the pure intent a strategy emits: which side to trade (or Flat to
// hold) and why. It carries no order construction details — no price, stop,
// take-profit, or units. Those are the planner's responsibility.
//
// Strength is reserved for future conviction-based sizing and may be zero.
type Signal struct {
	Side     market.Side
	Strength market.Rate // 0 = unset; planner ignores for now
	Reason   string
}

// Hold returns a Signal with Side == Flat, used to express "no action this bar".
func Hold(reason string) Signal {
	return Signal{Side: market.Flat, Reason: reason}
}
