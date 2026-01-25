package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
	"github.com/rustyeddy/trader/sim/replay"
)

func main() {
	var (
		ticksPath = flag.String("ticks", "", "CSV file of ticks (time,instrument,bid,ask)")
		dbPath    = flag.String("db", "./trader.db", "SQLite journal path")
		closeEnd  = flag.Bool("close-end", true, "close all open trades at end (ensures trade records exist)")
	)
	flag.Parse()

	if *ticksPath == "" {
		fmt.Fprintln(os.Stderr, "error: -ticks is required")
		os.Exit(2)
	}

	ctx := context.Background()

	j, err := journal.NewSQLite(*dbPath)
	if err != nil {
		panic(err)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-REPLAY",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	err = replay.CSV(ctx, *ticksPath, engine, replay.Options{TickThenEvent: true})
	if err != nil {
		panic(err)
	}

	if *closeEnd {
		// requires your CloseAll method
		if err := engine.CloseAll(ctx, "EndOfReplay"); err != nil {
			panic(err)
		}
	}

	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("DONE balance=%.2f equity=%.2f marginUsed=%.2f\n",
		acct.Balance, acct.Equity, acct.MarginUsed)
}
