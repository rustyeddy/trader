// Package strategy carries bars to a strategy running in another process on this
// machine and brings its answer back. It is the transport only: it knows nothing
// of orders, accounts or the engine, and it must not import the trader package.
//
// The protocol is two JSON messages, each preceded by its length as four
// big-endian bytes. For every group of bars the trader sends
//
//	{"bars":[...]}
//
// (marketdata.Bar as encoding/json writes it) and the worker answers
//
//	{"orders":[{"instrument":"EURUSD","action":"open","direction":"long",
//	            "price":"1.10000","stop_loss":"1.09000"}]}
//
// The orders may be empty. Each is a request (see Order): the trader decides the
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

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

type barsMessage struct {
	Bars []marketdata.Bar `json:"bars"`
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
	Instrument string `json:"instrument"`
	// Action is "open", "add", "close" or "reduce".
	Action string `json:"action"`
	// Direction is "long" or "short", for an open only.
	Direction string `json:"direction,omitempty"`
	// Type is "market" (the default when empty), "limit" or "stop".
	Type string `json:"type,omitempty"`
	// Price is the reference price of a market order, or the trigger of a limit
	// or stop order.
	Price num.Price `json:"price,omitzero"`
	// StopLoss is where the protective stop goes; an open requires it.
	StopLoss   num.Price `json:"stop_loss,omitzero"`
	TakeProfit num.Price `json:"take_profit,omitzero"`
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
