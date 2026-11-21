// Command remote-strategy is a tiny external strategy for Trader. It listens on a
// loopback port and, for every group of bars Trader sends, applies one fixed
// rule: open a long position in the first bar's instrument on the third group,
// with a stop a fixed distance below the close, and close it on the sixth. It
// asks only; Trader sizes the order, applies the account's risk rules and fills
// it.
//
// Run it, then point a Trader backtest at the address it prints:
//
//	go run ./examples/remote-strategy -addr 127.0.0.1:9100
//
// It serves one connection and exits when Trader closes it. The wire protocol is
// described in package strategy.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

const (
	openAt  = 3 // open on this group...
	closeAt = 6 // ...and close on this one
)

// stopDistance is how far below the entry the stop goes.
var stopDistance = num.MustParsePrice("0.015")

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "loopback address to listen on")
	flag.Parse()

	groups := 0
	err := strategy.ListenAndServe(*addr,
		func(a net.Addr) { fmt.Println("listening on", a) }, // for whoever started us
		func(bars []marketdata.Bar) ([]strategy.Order, error) {
			groups++
			return decide(groups, bars)
		})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "done: %d groups\n", groups)
}

// decide is the whole strategy: the orders to request on the n-th group of bars.
func decide(n int, bars []marketdata.Bar) ([]strategy.Order, error) {
	if len(bars) == 0 {
		return nil, nil
	}
	b := bars[0]
	closePrice := b.OHLC().Close
	switch n {
	case openAt:
		stop, err := closePrice.Sub(stopDistance)
		if err != nil {
			return nil, err
		}
		return []strategy.Order{{
			Instrument: b.Instrument, Action: "open", Direction: "long",
			Price: closePrice, StopLoss: stop,
		}}, nil
	case closeAt:
		return []strategy.Order{{Instrument: b.Instrument, Action: "close", Price: closePrice}}, nil
	}
	return nil, nil
}
