package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
)

// -----------------------------------------------------------------------------
// Backtest driver
// -----------------------------------------------------------------------------

func main() {
	var (
		ticksPath = flag.String("ticks", "", "path to tick CSV (time,instrument,bid,ask[,event...])")
		dbPath    = flag.String("db", "./backtest.sqlite", "path to SQLite journal DB")
		balance   = flag.Float64("balance", 100_000, "starting account balance/equity")
		acctID    = flag.String("account", "SIM-BACKTEST", "account ID for journaling")
		closeEnd  = flag.Bool("close-end", true, "close all open trades at end of replay")

		stratName  = flag.String("strategy", "noop", "strategy name (noop, open-once, ema-cross)")
		instrument = flag.String("instrument", "EUR_USD", "strategy instrument (used by some strategies)")
		units      = flag.Float64("units", 10_000, "order units (used by some strategies)")

		fast     = flag.Int("fast", 20, "ema-cross: fast EMA period")
		slow     = flag.Int("slow", 50, "ema-cross: slow EMA period")
		riskPct  = flag.Float64("risk", 0.005, "ema-cross: risk percent per trade (0.005 = 0.5%)")
		stopPips = flag.Float64("stop-pips", 20, "ema-cross: stop loss in pips")

		rr = flag.Float64("rr", 2.0, "ema-cross: take profit as R multiple (e.g. 2.0)")
	)
	flag.Parse()

	if *ticksPath == "" {
		fmt.Fprintln(os.Stderr, "error: -ticks is required")
		fmt.Fprintln(os.Stderr, "usage: backtest -ticks PATH [-db PATH] [-strategy noop|open-once]")
		os.Exit(2)
	}

	j, err := journal.NewSQLite(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       *acctID,
		Currency: "USD",
		Balance:  *balance,
		Equity:   *balance,
	}, j)

	strat, err := strategyByName(*stratName, *instrument, *units, *fast, *slow, *riskPct, *stopPips, *rr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "strategy: %v\n", err)
		os.Exit(2)
	}

	ctx := context.Background()
	if err := replayCSVWithStrategy(ctx, *ticksPath, engine, strat); err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}

	if *closeEnd {
		_ = engine.CloseAll(ctx, "EndOfReplay")
	}

	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("Done. balance=%.2f equity=%.2f margin_used=%.2f free_margin=%.2f\n",
		acct.Balance, acct.Equity, acct.MarginUsed, acct.FreeMargin)
}

// replayCSVWithStrategy reads a CSV and for each row:
//  1. engine.UpdatePrice(tick)
//  2. strat.OnTick(ctx, engine, tick)
//
// This guarantees your strategy sees the latest market state each tick.
func replayCSVWithStrategy(ctx context.Context, csvPath string, engine *sim.Engine, strat TickStrategy) error {
	f, err := os.Open(csvPath)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	firstRow, err := r.Read()
	if err != nil {
		return err
	}

	hasHeader := len(firstRow) > 0 && strings.EqualFold(strings.TrimSpace(firstRow[0]), "time")
	if !hasHeader {
		if err := handleRow(ctx, engine, strat, firstRow); err != nil {
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
		if len(row) == 0 {
			continue
		}
		if err := handleRow(ctx, engine, strat, row); err != nil {
			return err
		}
	}
}

func handleRow(ctx context.Context, engine *sim.Engine, strat TickStrategy, row []string) error {
	// Minimum tick columns: time,instrument,bid,ask
	if len(row) < 4 {
		return fmt.Errorf("bad row (need at least 4 cols time,instrument,bid,ask): %v", row)
	}

	ts := strings.TrimSpace(row[0])
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Support RFC3339Nano too (your downloader may output nanos)
		t2, err2 := time.Parse(time.RFC3339Nano, ts)
		if err2 != nil {
			return fmt.Errorf("bad time %q: %w", row[0], err)
		}
		t = t2
	}

	inst := strings.TrimSpace(row[1])

	bid, err := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
	if err != nil {
		return fmt.Errorf("bad bid %q: %w", row[2], err)
	}
	ask, err := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
	if err != nil {
		return fmt.Errorf("bad ask %q: %w", row[3], err)
	}

	tick := broker.Price{
		Time:       t,
		Instrument: inst,
		Bid:        bid,
		Ask:        ask,
	}

	// 1) Update market state (also triggers SL/TP evaluation + equity snapshots).
	if err := engine.UpdatePrice(tick); err != nil {
		return err
	}

	// 2) Call strategy for every tick.
	return strat.OnTick(ctx, engine, tick)
}
