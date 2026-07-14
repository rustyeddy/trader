package strategy

import "github.com/rustyeddy/trader/types"

// Signal is the pure intent a strategy emits each bar: which direction to
// trade (or Flat to hold) and why. It carries no order-construction details
// (no price, take-profit, or units) — those are the planner's job.
//
// CloseAll, when true, asks the planner to close all currently open lots
// before considering a new entry. This expresses time-based or
// signal-based exits that are not simple reversals.
//
// Stop is an optional suggested stop price. The exit strategy takes
// precedence when configured; this is used as a fallback when no exit
// strategy is active (e.g. for mechanical test strategies).
//
// Strength is reserved for future conviction-based sizing and may be zero.
type Signal struct {
	Side     types.Side
	Strength types.Rate  // 0 = unset; planner ignores for now
	CloseAll bool        // close all open lots before (re-)entering
	Stop     types.Price // optional suggested stop price; exit strategy overrides
	Reason   string
}

// Hold returns a Signal with Side == Flat, used to express "no action this bar".
func Hold(reason string) Signal {
	return Signal{Side: types.Flat, Reason: reason}
}
