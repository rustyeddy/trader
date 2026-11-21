// Command emax is Trader's canonical external-strategy example: the EMAX strategy
// as a separate process that Trader backtests over loopback TCP and JSON, using
// only the public Go SDK. See README.md.
//
//	go run ./examples/emax -addr 127.0.0.1:9100
//	trader backtest --worker 127.0.0.1:9100 --instrument EURUSD --interval h1 \
//	    --from 2024-01-01 --to 2024-04-01
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/rustyeddy/trader/examples/emax/emax"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "loopback address to listen on")
	short := flag.Int("short", 2, "short EMA window")
	long := flag.Int("long", 4, "long EMA window")
	stop := flag.String("stop", "0.01", "stop distance from the entry, as a fraction of the price")
	tick := flag.String("tick", "0.00001", "price tick of the instruments traded (EURUSD and GBPUSD: 0.00001)")
	flag.Parse()

	cfg := emax.Config{Short: *short, Long: *long}
	var err error
	if cfg.StopPct, err = num.ParseRate(*stop); err != nil {
		log.Fatalf("-stop: %v", err)
	}
	if cfg.Tick, err = num.ParsePrice(*tick); err != nil {
		log.Fatalf("-tick: %v", err)
	}
	s, err := emax.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	groups := 0
	err = strategy.ListenAndServe(*addr,
		func(a net.Addr) { fmt.Println("listening on", a) }, // for whoever started us
		func(bars []marketdata.Bar, events []strategy.Event) ([]strategy.Order, error) {
			groups++
			return s.OnBars(bars, events)
		})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "done: %d groups\n", groups)
}
