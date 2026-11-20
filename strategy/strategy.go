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
//	{"orders":[]}
//
// Orders are not carried yet: the answer must be empty.
//
// The trader side is Dial and Client; the worker side is ListenAndServe, or Serve
// for a connection already in hand. Both sides are loopback-only: nothing here is
// authenticated or encrypted.
package strategy

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/rustyeddy/trader/marketdata"
)

type barsMessage struct {
	Bars []marketdata.Bar `json:"bars"`
}

type ordersMessage struct {
	Orders []json.RawMessage `json:"orders"`
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
