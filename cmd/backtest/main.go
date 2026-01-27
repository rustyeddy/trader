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

// Config holds all command-line flag values for the backtest application.
type Config struct {
	// Data and persistence
	TicksPath string
	DBPath    string

	// Account configuration
	Balance float64
	AcctID  string

	// Simulation control
	CloseEnd bool

	// Strategy configuration
	StratName  string
	Instrument string
	Units      float64

	// EMA-cross strategy parameters
	Fast     int
	Slow     int
	RiskPct  float64
	StopPips float64
	RR       float64
}

// TickStrategy is the minimal interface a backtest strategy must implement.
// It is called once per CSV row (tick).
type TickStrategy interface {
	OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error
}

// -----------------------------------------------------------------------------
// Strategy registry (start tiny; add more as you build real strategies)
// -----------------------------------------------------------------------------

func strategyByName(name string, instrument string, units float64, fast, slow int, riskPct, stopPips, rr float64) (TickStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "noop", "none":
		return NoopStrategy{}, nil

	case "open-once":
		return &OpenOnceStrategy{
			Instrument: instrument,
			Units:      units,
		}, nil

	case "ema-cross", "emacross":
		return NewEmaCrossStrategy(instrument, fast, slow, riskPct, stopPips, rr), nil

	default:
		return nil, fmt.Errorf("unknown strategy %q (supported: noop, open-once)", name)
	}
}

// NoopStrategy does nothing.
type NoopStrategy struct{}

func (NoopStrategy) OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error {
	_ = ctx
	_ = b
	_ = tick
	return nil
}

// OpenOnceStrategy opens a single market order the first time it sees a tick
// for the configured instrument. It's meant as a wiring test.
type OpenOnceStrategy struct {
	Instrument string
	Units      float64

	opened bool
}

func (s *OpenOnceStrategy) OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error {
	if s.opened {
		return nil
	}
	if tick.Instrument != s.Instrument {
		return nil
	}
	if s.Units == 0 {
		return fmt.Errorf("open-once: units must be non-zero")
	}

	_, err := b.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: s.Instrument,
		Units:      s.Units,
	})
	if err != nil {
		return err
	}
	s.opened = true
	return nil
}

// -----------------------------------------------------------------------------
// Backtest driver
// -----------------------------------------------------------------------------

func main() {
	cfg := Config{}

	// Define flags using Config fields
	flag.StringVar(&cfg.TicksPath, "ticks", "", "path to tick CSV (time,instrument,bid,ask[,event...])")
	flag.StringVar(&cfg.DBPath, "db", "./backtest.sqlite", "path to SQLite journal DB")
	flag.Float64Var(&cfg.Balance, "balance", 100_000, "starting account balance/equity")
	flag.StringVar(&cfg.AcctID, "account", "SIM-BACKTEST", "account ID for journaling")
	flag.BoolVar(&cfg.CloseEnd, "close-end", true, "close all open trades at end of replay")

	flag.StringVar(&cfg.StratName, "strategy", "noop", "strategy name (noop, open-once, ema-cross)")
	flag.StringVar(&cfg.Instrument, "instrument", "EUR_USD", "strategy instrument (used by some strategies)")
	flag.Float64Var(&cfg.Units, "units", 10_000, "order units (used by some strategies)")

	flag.IntVar(&cfg.Fast, "fast", 20, "ema-cross: fast EMA period")
	flag.IntVar(&cfg.Slow, "slow", 50, "ema-cross: slow EMA period")
	flag.Float64Var(&cfg.RiskPct, "risk", 0.005, "ema-cross: risk percent per trade (0.005 = 0.5%)")
	flag.Float64Var(&cfg.StopPips, "stop-pips", 20, "ema-cross: stop loss in pips")
	flag.Float64Var(&cfg.RR, "rr", 2.0, "ema-cross: take profit as R multiple (e.g. 2.0)")

	flag.Parse()

	if cfg.TicksPath == "" {
		fmt.Fprintln(os.Stderr, "error: -ticks is required")
		fmt.Fprintln(os.Stderr, "usage: backtest -ticks PATH [-db PATH] [-strategy noop|open-once]")
		os.Exit(2)
	}

	j, err := journal.NewSQLite(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       cfg.AcctID,
		Currency: "USD",
		Balance:  cfg.Balance,
		Equity:   cfg.Balance,
	}, j)

	strat, err := strategyByName(cfg.StratName, cfg.Instrument, cfg.Units, cfg.Fast, cfg.Slow, cfg.RiskPct, cfg.StopPips, cfg.RR)
	if err != nil {
		fmt.Fprintf(os.Stderr, "strategy: %v\n", err)
		os.Exit(2)
	}

	ctx := context.Background()
	if err := replayCSVWithStrategy(ctx, cfg.TicksPath, engine, strat); err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}

	if cfg.CloseEnd {
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
