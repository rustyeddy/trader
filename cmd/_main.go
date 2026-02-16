package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/broker/sim"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/replay"
	"github.com/rustyeddy/trader/risk"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	sub := os.Args[1]
	switch sub {
	case "run":
		os.Exit(runCmd(os.Args[2:]))
	case "replay":
		os.Exit(replayCmd(os.Args[2:]))
	case "config":
		os.Exit(configCmd(os.Args[2:]))
	case "journal":
		os.Exit(journalCmd(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", sub)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `trader - CLI utilities for the trader project

Commands:
  run      Run a simulation from a config file
  replay   Replay historical tick data from CSV or config
  config   Generate or validate configuration files
  journal  Query trade journal data

Usage:
  trader run -config PATH
  trader replay -ticks PATH [-db PATH] [-close-end]
  trader replay -config PATH
  trader config init [-output PATH]
  trader config validate -config PATH
  trader journal -db PATH trade <trade_id>
  trader journal -db PATH today
  trader journal -db PATH day YYYY-MM-DD

Examples:
  trader run -config examples/configs/basic.yaml
  trader replay -ticks data/ticks.csv -db replay.sqlite
  trader replay -config replay-config.yaml
  trader config init -output my-simulation.yaml
  trader journal -db ./trader.sqlite trade 3f2b5c12-....
  trader journal -db ./trader.sqlite today`)
}

func journalCmd(args []string) int {
	fs := flag.NewFlagSet("journal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var dbPath string
	fs.StringVar(&dbPath, "db", "./trader.sqlite", "path to SQLite journal DB")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	rest := fs.Args()
	if len(rest) < 1 {
		usage()
		return 2
	}

	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		return 1
	}
	defer j.Close()

	loc := time.Local

	switch rest[0] {
	case "trade":
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "journal trade requires a trade_id")
			return 2
		}
		rec, err := j.GetTrade(rest[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		fmt.Println(journal.FormatTradeOrg(rec))
		return 0

	case "today":
		start, end, err := dayBounds(loc, time.Now().In(loc).Format("2006-01-02"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "date: %v\n", err)
			return 1
		}
		recs, err := j.ListTradesClosedBetween(start, end)
		if err != nil {
			fmt.Fprintf(os.Stderr, "query trades: %v\n", err)
			return 1
		}
		fmt.Println(journal.FormatTradesOrg(recs))
		return 0

	case "day":
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "journal day requires YYYY-MM-DD")
			return 2
		}
		start, end, err := dayBounds(loc, rest[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "date: %v\n", err)
			return 1
		}
		recs, err := j.ListTradesClosedBetween(start, end)
		if err != nil {
			fmt.Fprintf(os.Stderr, "query trades: %v\n", err)
			return 1
		}
		fmt.Println(journal.FormatTradesOrg(recs))
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown journal command: %s\n\n", rest[0])
		usage()
		return 2
	}
}

func dayBounds(loc *time.Location, day string) (time.Time, time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	end := start.Add(24 * time.Hour)
	return start, end, nil
}

func runCmd(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var configPath string
	fs.StringVar(&configPath, "config", "", "path to config file (YAML or JSON)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if configPath == "" {
		fmt.Fprintln(os.Stderr, "error: -config flag is required")
		fmt.Fprintln(os.Stderr, "usage: trader run -config PATH")
		return 2
	}

	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}

	fmt.Printf("Running simulation with config: %s\n", configPath)
	fmt.Printf("  Account: %s (Balance: $%.2f %s)\n", cfg.Account.ID, cfg.Account.Balance, cfg.Account.Currency)
	fmt.Printf("  Strategy: %s (Risk: %.1f%%, Stop: %.0f pips, Target: %.0f pips)\n",
		cfg.Strategy.Instrument, cfg.Strategy.RiskPercent*100, cfg.Strategy.StopPips, cfg.Strategy.TargetPips)
	fmt.Println()

	// Create journal
	var j journal.Journal
	if cfg.Journal.Type == "csv" {
		j, err = journal.NewCSV(cfg.Journal.TradesFile, cfg.Journal.EquityFile)
	} else {
		j, err = journal.NewSQLite(cfg.Journal.DBPath)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "create journal: %v\n", err)
		return 1
	}
	defer j.Close()

	// Initialize engine
	engine := sim.NewEngine(broker.Account{
		ID:       cfg.Account.ID,
		Currency: cfg.Account.Currency,
		Balance:  cfg.Account.Balance,
		Equity:   cfg.Account.Balance,
	}, j)

	// Set initial price
	engine.Prices().Set(broker.Price{
		Instrument: cfg.Strategy.Instrument,
		Bid:        cfg.Simulation.InitialBid,
		Ask:        cfg.Simulation.InitialAsk,
		Time:       time.Now(),
	})

	ctx := context.Background()
	acct, _ := engine.GetAccount(ctx)
	price, _ := engine.GetPrice(ctx, cfg.Strategy.Instrument)
	meta, ok := market.Instruments[cfg.Strategy.Instrument]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown instrument: %s\n", cfg.Strategy.Instrument)
		return 1
	}

	// Calculate position size using dynamic pip size calculation
	pipSize := risk.PipSize(meta.PipLocation)
	stopPrice := price.Ask - (cfg.Strategy.StopPips * pipSize)

	var quoteToAccount float64
	if meta.QuoteCurrency == "USD" {
		quoteToAccount = 1.0
	} else if meta.QuoteCurrency == "JPY" {
		quoteToAccount = 1.0 / price.Mid()
	} else {
		// For other currencies, default to 1.0 (assumes quote currency = account currency)
		quoteToAccount = 1.0
	}

	size := risk.Calculate(risk.Inputs{
		Equity:         acct.Equity,
		RiskPct:        cfg.Strategy.RiskPercent,
		EntryPrice:     price.Ask,
		StopPrice:      stopPrice,
		PipLocation:    meta.PipLocation,
		QuoteToAccount: quoteToAccount,
	})

	targetPrice := price.Ask + (cfg.Strategy.TargetPips * pipSize)

	fmt.Printf("Opening trade:\n")
	fmt.Printf("  Entry: %.4f\n", price.Ask)
	fmt.Printf("  Stop: %.4f (%.0f pips)\n", stopPrice, cfg.Strategy.StopPips)
	fmt.Printf("  Target: %.4f (%.0f pips)\n", targetPrice, cfg.Strategy.TargetPips)
	fmt.Printf("  Position Size: %.0f units\n", size.Units)
	fmt.Printf("  Risk Amount: $%.2f\n\n", size.RiskAmount)

	// Open trade
	_, err = engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: cfg.Strategy.Instrument,
		Units:      size.Units,
		StopLoss:   &stopPrice,
		TakeProfit: &targetPrice,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create order: %v\n", err)
		return 1
	}

	// Apply price steps if configured
	for i, step := range cfg.Simulation.PriceSteps {
		delay, err := step.ParseDuration()
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid delay in step %d: %v\n", i, err)
			return 1
		}

		fmt.Printf("Updating price to Bid: %.4f, Ask: %.4f (after %s)\n", step.Bid, step.Ask, delay)
		err = engine.UpdatePrice(broker.Price{
			Instrument: cfg.Strategy.Instrument,
			Bid:        step.Bid,
			Ask:        step.Ask,
			Time:       time.Now().Add(delay),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "update price: %v\n", err)
			return 1
		}
	}

	// Final account state
	acct, _ = engine.GetAccount(ctx)
	fmt.Printf("\nFinal Results:\n")
	fmt.Printf("  Balance: $%.2f\n", acct.Balance)
	fmt.Printf("  Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Profit/Loss: $%.2f\n", acct.Equity-cfg.Account.Balance)
	if cfg.Journal.Type == "csv" {
		fmt.Printf("\nResults saved to:\n  - %s\n  - %s\n", cfg.Journal.TradesFile, cfg.Journal.EquityFile)
	} else {
		fmt.Printf("\nResults saved to: %s\n", cfg.Journal.DBPath)
	}

	return 0
}

func replayCmd(args []string) int {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		configPath = fs.String("config", "", "path to config file with replay settings")
		ticksPath  = fs.String("ticks", "", "CSV file of ticks (time,instrument,bid,ask)")
		dbPath     = fs.String("db", "./trader.sqlite", "SQLite journal path")
		closeEnd   = fs.Bool("close-end", true, "close all open trades at end")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()

	// Config-based replay
	if *configPath != "" {
		return replayFromConfig(ctx, *configPath)
	}

	// Direct CSV replay
	if *ticksPath == "" {
		fmt.Fprintln(os.Stderr, "error: either -config or -ticks flag is required")
		fmt.Fprintln(os.Stderr, "usage: trader replay -ticks PATH [-db PATH] [-close-end]")
		fmt.Fprintln(os.Stderr, "   or: trader replay -config PATH")
		return 2
	}

	j, err := journal.NewSQLite(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create journal: %v\n", err)
		return 1
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-REPLAY",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	fmt.Printf("Replaying ticks from: %s\n", *ticksPath)
	err = replay.CSV(ctx, *ticksPath, engine, replay.Options{TickThenEvent: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay error: %v\n", err)
		return 1
	}

	if *closeEnd {
		if err := engine.CloseAll(ctx, "EndOfReplay"); err != nil {
			fmt.Fprintf(os.Stderr, "close all: %v\n", err)
			return 1
		}
	}

	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("\nReplay complete!\n")
	fmt.Printf("  Balance: $%.2f\n", acct.Balance)
	fmt.Printf("  Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Margin Used: $%.2f\n", acct.MarginUsed)
	fmt.Printf("\nResults saved to: %s\n", *dbPath)

	return 0
}

func replayFromConfig(ctx context.Context, configPath string) int {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}

	if cfg.Replay == nil || cfg.Replay.CSVFile == "" {
		fmt.Fprintln(os.Stderr, "error: config must include replay.csv_file")
		return 1
	}

	fmt.Printf("Replaying with config: %s\n", configPath)
	fmt.Printf("  Account: %s (Balance: $%.2f %s)\n", cfg.Account.ID, cfg.Account.Balance, cfg.Account.Currency)
	fmt.Printf("  CSV File: %s\n", cfg.Replay.CSVFile)
	fmt.Println()

	// Create journal
	var j journal.Journal
	if cfg.Journal.Type == "csv" {
		j, err = journal.NewCSV(cfg.Journal.TradesFile, cfg.Journal.EquityFile)
	} else {
		j, err = journal.NewSQLite(cfg.Journal.DBPath)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "create journal: %v\n", err)
		return 1
	}
	defer j.Close()

	// Initialize engine
	engine := sim.NewEngine(broker.Account{
		ID:       cfg.Account.ID,
		Currency: cfg.Account.Currency,
		Balance:  cfg.Account.Balance,
		Equity:   cfg.Account.Balance,
	}, j)

	// Run replay
	err = replay.CSV(ctx, cfg.Replay.CSVFile, engine, replay.Options{
		TickThenEvent: cfg.Replay.TickThenEvent,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay error: %v\n", err)
		return 1
	}

	// Close all trades at end if configured
	if cfg.Replay.CloseAtEnd {
		if err := engine.CloseAll(ctx, "EndOfReplay"); err != nil {
			fmt.Fprintf(os.Stderr, "close all: %v\n", err)
			return 1
		}
	}

	// Final account state
	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("\nReplay complete!\n")
	fmt.Printf("  Balance: $%.2f\n", acct.Balance)
	fmt.Printf("  Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Profit/Loss: $%.2f\n", acct.Equity-cfg.Account.Balance)
	if cfg.Journal.Type == "csv" {
		fmt.Printf("\nResults saved to:\n  - %s\n  - %s\n", cfg.Journal.TradesFile, cfg.Journal.EquityFile)
	} else {
		fmt.Printf("\nResults saved to: %s\n", cfg.Journal.DBPath)
	}

	return 0
}

func configCmd(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "config command requires a subcommand: init or validate")
		return 2
	}

	sub := args[0]
	switch sub {
	case "init":
		return configInitCmd(args[1:])
	case "validate":
		return configValidateCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", sub)
		return 2
	}
}

func configInitCmd(args []string) int {
	fs := flag.NewFlagSet("config init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var output string
	fs.StringVar(&output, "output", "simulation.yaml", "output config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg := config.Default()
	if err := cfg.SaveToFile(output); err != nil {
		fmt.Fprintf(os.Stderr, "save config: %v\n", err)
		return 1
	}

	fmt.Printf("Created default configuration: %s\n", output)
	fmt.Println("\nEdit the file and run with:")
	fmt.Printf("  trader run -config %s\n", output)
	return 0
}

func configValidateCmd(args []string) int {
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var configPath string
	fs.StringVar(&configPath, "config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if configPath == "" {
		fmt.Fprintln(os.Stderr, "error: -config flag is required")
		return 2
	}

	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validation failed: %v\n", err)
		return 1
	}

	fmt.Printf("✓ Configuration valid: %s\n", configPath)
	fmt.Printf("  Account: %s ($%.2f %s)\n", cfg.Account.ID, cfg.Account.Balance, cfg.Account.Currency)
	fmt.Printf("  Strategy: %s (Risk: %.1f%%)\n", cfg.Strategy.Instrument, cfg.Strategy.RiskPercent*100)
	fmt.Printf("  Journal: %s\n", cfg.Journal.Type)
	return 0
}

