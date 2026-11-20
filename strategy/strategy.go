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
// The trader side is Dial and Remote; the worker side is Serve.
package strategy

import (
	"encoding/json"

	"github.com/rustyeddy/trader/marketdata"
)

type barsMessage struct {
	Bars []marketdata.Bar `json:"bars"`
}

type ordersMessage struct {
	Orders []json.RawMessage `json:"orders"`
}
