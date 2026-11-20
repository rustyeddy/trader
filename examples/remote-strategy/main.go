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

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("listening on", ln.Addr()) // the address, for whoever started us
	conn, err := ln.Accept()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	groups := 0
	err = strategy.Serve(conn, func([]marketdata.Bar) error {
		// A real strategy decides here. This one only counts.
		groups++
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "done: %d groups\n", groups)
}
