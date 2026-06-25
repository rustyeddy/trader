package strategy

import "github.com/rustyeddy/trader/market"

// Signal is the pure intent a strategy emits each bar: which direction to
// trade (or Flat to hold) and why. It carries no order-construction details
// (no price, stop, take-profit, or units) — those are the planner's job.
//
// CloseAll, when true, asks the planner to close all currently open lots
// before considering a new entry. This expresses time-based or
// signal-based exits that are not simple reversals.
//
// Strength is reserved for future conviction-based sizing and may be zero.
type Signal struct {
	Side     market.Side
	Strength market.Rate // 0 = unset; planner ignores for now
	CloseAll bool        // close all open lots before (re-)entering
	Reason   string
}

// Hold returns a Signal with Side == Flat, used to express "no action this bar".
func Hold(reason string) Signal {
	return Signal{Side: market.Flat, Reason: reason}
}
