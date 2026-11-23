// Command emax is Trader's canonical external-strategy example: the EMAX strategy
// as a separate process that connects to a running Trader, asks it for one
// backtest, trades it over loopback TCP and JSON, and prints the result. It uses
// only the public Go SDK. See README.md.
//
//	trader serve                       # Trader, in another terminal
//	go run ./examples/emax -instrument EURUSD -interval h1 -from 2024-01-01 -to 2024-04-01
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/rustyeddy/trader/examples/emax/emax"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

func main() {
	addr := flag.String("trader", "127.0.0.1:9100", "Trader's strategy address (trader serve --strategy-listen), loopback only")
	instruments := flag.String("instrument", "EURUSD", "instruments to trade, separated by commas")
	interval := flag.String("interval", "h1", "h1 or d1")
	from := flag.String("from", "", "first instant, UTC: YYYY-MM-DD or RFC 3339 (required)")
	to := flag.String("to", "", "end of the range, exclusive, UTC: YYYY-MM-DD or RFC 3339 (required)")
	short := flag.Int("short", 2, "short EMA window")
	long := flag.Int("long", 4, "long EMA window")
	stop := flag.String("stop", "0.01", "stop distance from the entry, as a fraction of the price")
	tick := flag.String("tick", "0.00001", "price tick of the instruments traded (EURUSD and GBPUSD: 0.00001)")
	wait := flag.Duration("wait", strategy.DefaultTimeout, "how long to wait for Trader to accept the run, if it is busy with another")
	flag.Parse()

	req := strategy.RunRequest{Instruments: strings.Split(*instruments, ","), Interval: *interval}
	var err error
	if req.From, err = parseTime(*from); err != nil {
		log.Fatalf("-from: %v", err)
	}
	if req.To, err = parseTime(*to); err != nil {
		log.Fatalf("-to: %v", err)
	}

	cfg := emax.Config{Short: *short, Long: *long}
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// OnBars trades; Warm learns from the bars before the range and does nothing else.
	res, err := strategy.Run(ctx, *addr, req, s.OnBars, strategy.WithWarmup(s.Warm), strategy.WithQueueTimeout(*wait))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("run %s: %s\n", res.RunID, res.Status)
	if res.Error != "" {
		fmt.Printf("error: %s\n", res.Error)
		os.Exit(1)
	}
	fmt.Printf("%s\n", res.Summary)
}

// parseTime reads a UTC date or instant.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("required")
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
