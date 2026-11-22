// Package strategy carries bars to a strategy running in another process on this
// machine and brings its answer back. It is the transport only: it knows nothing
// of orders, accounts or the engine, and it must not import the trader package.
//
// The protocol is two JSON messages, each preceded by its length as four
// big-endian bytes. For every group of bars the trader sends
//
//	{"bars":[...],"events":[...]}
//
// (marketdata.Bar as encoding/json writes it; the events are what the trader did
// since the last group, see Event, and may be empty) and the worker answers
//
//	{"orders":[{"instrument":"EURUSD","action":"open","direction":"long",
//	            "price":"1.10000","stop_loss":"1.09000"}]}
//
// Before the first group the trader may send the bars that precede the range it
// trades, each group marked "warmup":true, with no events. They are for the worker
// to prefill its indicators from; it answers {"orders":[]}, and an answer with an
// order is an error, since nothing is traded on them. A worker that does not use
// history ignores them (see WithWarmup).
//
// The list must be present, but may be empty; an answer without it, or with
// null, is an error. Each order is a request (see Order): the trader decides the
// size, whether to admit it, and everything that follows. A field the protocol
// does not define, such as units, is an error, not ignored.
//
// The trader side is Dial and Client; the worker side is ListenAndServe, or Serve
// for a connection already in hand. Both sides are loopback-only: nothing here is
// authenticated or encrypted.
package strategy

import (
	"fmt"
	"net"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

type barsMessage struct {
	Bars   []marketdata.Bar `json:"bars"`
	Events []Event          `json:"events"`
	// Warmup marks bars from before the range being traded: the worker learns from
	// them (fills its indicators) and answers with no orders.
	Warmup bool `json:"warmup,omitempty"`
}

type ordersMessage struct {
	Orders []Order `json:"orders"`
}

// Order is what a worker may ask for. It is a request on the wire, not the
// trader's order: the trader maps it onto its own order, sizes it, applies the
// account's risk rules and the broker executes it. A worker cannot say how many
// units, nor name an id, account, state or fill; such a field is refused.
//
// Prices are decimal strings. Action and Direction use the trader's own words.
type Order struct {
	Instrument string    `json:"instrument"`
	Action     string    `json:"action"`              // Action is "open", "add", "close" or "reduce".
	Direction  string    `json:"direction,omitempty"` // Direction is "long" or "short", for an open only.
	Type       string    `json:"type,omitempty"`      // Type is "market" (the default when empty), "limit" or "stop".
	Price      num.Price `json:"price,omitzero"`      // the reference price of a market order, or the trigger of a limit or stop order.
	StopLoss   num.Price `json:"stop_loss,omitzero"`  // StopLoss is where the protective stop goes; an open requires it.
	TakeProfit num.Price `json:"take_profit,omitzero"`
	// Ref is the worker's own label for the order. The trader echoes it on the
	// order's events so the worker can tell which request an event is about. It
	// means nothing to the trader.
	Ref string `json:"ref,omitempty"`
}

// Event kinds.
const (
	Accepted  = "accepted"  // the trader sized the order and queued it with the broker
	Filled    = "filled"    // the broker executed it
	Rejected  = "rejected"  // the trader or broker refused it; Reason says why
	Cancelled = "cancelled" // it was accepted, then withdrawn before it filled
)

// Event is something the trader did, reported to the worker with the next group
// of bars. It is a read-only fact: the worker cannot change it, and it carries no
// account or position state. Each event is delivered once, in the order it
// happened, and never before the trader knew it: an order the worker requests in
// one answer is first reported with the group after it.
type Event struct {
	// Time is when it happened: the opening time of the bar it happened on.
	Time time.Time `json:"time"`
	// Kind is one of Accepted, Filled, Rejected, Cancelled.
	Kind string `json:"kind"`
	// ID is the order's id, assigned by the trader. It is empty for an order
	// refused before it was queued, which never gets one.
	ID string `json:"id,omitempty"`
	// Ref is the Ref the worker gave the order; empty for an order the trader's
	// broker made itself, such as a protective stop.
	Ref        string `json:"ref,omitempty"`
	Instrument string `json:"instrument"`
	Action     string `json:"action"`
	Direction  string `json:"direction,omitempty"`
	// Units is the size the trader chose, for Accepted and Filled.
	Units num.Quantity `json:"units,omitzero"`
	// Price is the fill price, for Filled.
	Price num.Price `json:"price,omitzero"`
	// Cause is why the order exists: "strategy" for a worker's order, or
	// "stop loss", "take profit" or "margin closeout" for one the broker made. A
	// Filled event with cause "stop loss" is a protective-stop exit.
	Cause string `json:"cause"`
	// Reason says why, for Rejected.
	Reason string `json:"reason,omitempty"`
}

// requireLoopback refuses an address that is not on this machine's loopback
// interface. A host name other than localhost is refused too, since it may
// resolve anywhere.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("address %q is not loopback", addr)
	}
	return nil
}
