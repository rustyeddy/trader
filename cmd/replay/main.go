package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
	"github.com/rustyeddy/trader/strategy"
)

func main() {
	var (
		ticksPath = flag.String("ticks", "", "CSV file of ticks (time,instrument,bid,ask)")
		dbPath    = flag.String("db", "./trader.db", "SQLite journal path")
		openOnce  = flag.Bool("open-once", true, "open one starter trade at the beginning (demo)")
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

	firstTick := true

	if err := replayTicks(*ticksPath, func(p broker.Price) error {
		// Feed tick into engine
		if err := engine.UpdatePrice(p); err != nil {
			return err
		}

		// Optional: open a starter trade once prices exist
		if firstTick && *openOnce {
			firstTick = false
			// demo strategy uses EUR_USD; if your first tick isn't EUR_USD, just set open-once=false
			_ = strategy.TradeEURUSD(ctx, engine)
		}
		return nil
	}); err != nil {
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

func replayTicks(path string, onTick func(broker.Price) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	// Read header (and tolerate files without it)
	header, err := r.Read()
	if err != nil {
		return err
	}

	hasHeader := len(header) >= 4 && header[0] == "time" && header[1] == "instrument"
	if !hasHeader {
		// Treat the first row as a tick row.
		if err := handleRow(header, onTick); err != nil {
			return err
		}
	}

	for {
		row, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := handleRow(row, onTick); err != nil {
			return err
		}
	}
}

func handleRow(row []string, onTick func(broker.Price) error) error {
	if len(row) < 4 {
		return fmt.Errorf("bad row (need 4 columns): %v", row)
	}

	t, err := time.Parse(time.RFC3339, row[0])
	if err != nil {
		return fmt.Errorf("bad time %q: %w", row[0], err)
	}
	inst := row[1]

	bid, err := strconv.ParseFloat(row[2], 64)
	if err != nil {
		return fmt.Errorf("bad bid %q: %w", row[2], err)
	}
	ask, err := strconv.ParseFloat(row[3], 64)
	if err != nil {
		return fmt.Errorf("bad ask %q: %w", row[3], err)
	}

	return onTick(broker.Price{
		Time:       t,
		Instrument: inst,
		Bid:        bid,
		Ask:        ask,
	})
}
