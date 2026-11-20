// Command remote-strategy is a minimal external strategy for Trader: it listens
// on a loopback port, takes each group of bars Trader sends and always answers
// with no orders.
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
	"github.com/rustyeddy/trader/strategy"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "loopback address to listen on")
	flag.Parse()

	groups := 0
	err := strategy.ListenAndServe(*addr,
		func(a net.Addr) { fmt.Println("listening on", a) }, // for whoever started us
		func([]marketdata.Bar) error {
			// A real strategy decides here. This one only counts.
			groups++
			return nil
		})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "done: %d groups\n", groups)
}
