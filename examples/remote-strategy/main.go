// Command remote-strategy is a tiny external strategy for Trader. It listens on a
// loopback port and, for every group of bars Trader sends, applies one fixed
// rule: open a long position in the first bar's instrument on the third group,
// with a stop a fixed distance below the close, and close it on the sixth, but
// only if Trader has told it the entry filled and the stop has not already taken
// it out. It asks only; Trader sizes the order, applies the account's risk rules
// and fills it, and reports what it did with the next group.
//
// Run it, then point a Trader backtest at the address it prints:
//
//	go run ./examples/remote-strategy -addr 127.0.0.1:9100
//
// It prints each event Trader reports to standard error. It serves one
// connection and exits when Trader closes it. The wire protocol is described in
// package strategy.
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
	closeAt = 6 // ...and close on this one, if still in
)

// entryRef labels the opening order, so its events can be recognised.
const entryRef = "entry"

// stopDistance is how far below the entry the stop goes.
var stopDistance = num.MustParsePrice("0.015")

// worker is the strategy's memory: how many groups it has seen and whether it is
// in a position, as far as Trader has said.
type worker struct {
	groups     int
	inPosition bool
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "loopback address to listen on")
	flag.Parse()

	w := &worker{}
	err := strategy.ListenAndServe(*addr,
		func(a net.Addr) { fmt.Println("listening on", a) }, // for whoever started us
		func(bars []marketdata.Bar, events []strategy.Event) ([]strategy.Order, error) {
			for _, e := range events {
				fmt.Fprintf(os.Stderr, "event: %s %s %s %s %s ref=%q id=%q reason=%q\n",
					e.Time.Format("2006-01-02"), e.Kind, e.Action, e.Instrument, e.Cause, e.Ref, e.ID, e.Reason)
			}
			return w.decide(bars, events)
		})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "done: %d groups\n", w.groups)
}

// decide is the whole strategy: what Trader reported, then the orders to request
// on the next group of bars.
func (w *worker) decide(bars []marketdata.Bar, events []strategy.Event) ([]strategy.Order, error) {
	for _, e := range events {
		switch {
		case e.Ref == entryRef && e.Kind == strategy.Filled:
			w.inPosition = true
		case e.Kind == strategy.Filled && e.Cause == "stop loss":
			w.inPosition = false // the protective stop took us out
		}
	}
	w.groups++
	if len(bars) == 0 {
		return nil, nil
	}
	b := bars[0]
	closePrice := b.OHLC().Close
	switch {
	case w.groups == openAt:
		stop, err := closePrice.Sub(stopDistance)
		if err != nil {
			return nil, err
		}
		return []strategy.Order{{
			Instrument: b.Instrument, Action: "open", Direction: "long",
			Price: closePrice, StopLoss: stop, Ref: entryRef,
		}}, nil
	case w.groups == closeAt && w.inPosition:
		return []strategy.Order{{Instrument: b.Instrument, Action: "close", Price: closePrice}}, nil
	}
	return nil, nil
}
