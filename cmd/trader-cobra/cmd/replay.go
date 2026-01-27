package cmd

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/replay"
	"github.com/rustyeddy/trader/sim"
	"github.com/spf13/cobra"
)

var replayCmd = &cobra.Command{
	Use:   "replay",
	Short: "Replay historical tick data from CSV",
	Long: `Replay tick data from CSV files to test the trading engine.

Can replay from a simple CSV file or from a configuration file.

Examples:
  trader replay -ticks data/eurusd.csv
  trader replay -config examples/configs/replay.yaml`,
	RunE: runReplay,
}

var (
	replayConfigPath string
	replayTicksPath  string
	replayDBPath     string
	replayCloseEnd   bool
)

func init() {
	rootCmd.AddCommand(replayCmd)

	replayCmd.Flags().StringVarP(&replayConfigPath, "config", "f", "", "path to config file with replay settings")
	replayCmd.Flags().StringVarP(&replayTicksPath, "ticks", "t", "", "CSV file of ticks (time,instrument,bid,ask)")
	replayCmd.Flags().StringVarP(&replayDBPath, "db", "d", "./trader.sqlite", "SQLite journal path")
	replayCmd.Flags().BoolVar(&replayCloseEnd, "close-end", true, "close all open trades at end")
}

func runReplay(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if replayConfigPath != "" {
		return replayFromConfig(ctx, replayConfigPath)
	}

	if replayTicksPath == "" {
		return fmt.Errorf("either -config or -ticks flag is required")
	}

	j, err := journal.NewSQLite(replayDBPath)
	if err != nil {
		return fmt.Errorf("create journal: %w", err)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-REPLAY",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	fmt.Printf("Replaying ticks from: %s\n", replayTicksPath)
	err = replay.CSV(ctx, replayTicksPath, engine, replay.Options{TickThenEvent: true})
	if err != nil {
		return fmt.Errorf("replay error: %w", err)
	}

	if replayCloseEnd {
		if err := engine.CloseAll(ctx, "EndOfReplay"); err != nil {
			return fmt.Errorf("close all: %w", err)
		}
	}

	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("\nReplay complete!\n")
	fmt.Printf("  Balance: $%.2f\n", acct.Balance)
	fmt.Printf("  Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Margin Used: $%.2f\n", acct.MarginUsed)
	fmt.Printf("\nResults saved to: %s\n", replayDBPath)

	return nil
}

func replayFromConfig(ctx context.Context, configPath string) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Replay == nil || cfg.Replay.CSVFile == "" {
		return fmt.Errorf("config must include replay.csv_file")
	}

	fmt.Printf("Replaying with config: %s\n", configPath)
	fmt.Printf("  Account: %s (Balance: $%.2f %s)\n", cfg.Account.ID, cfg.Account.Balance, cfg.Account.Currency)
	fmt.Printf("  CSV File: %s\n", cfg.Replay.CSVFile)
	fmt.Println()

	var j journal.Journal
	if cfg.Journal.Type == "csv" {
		j, err = journal.NewCSV(cfg.Journal.TradesFile, cfg.Journal.EquityFile)
	} else {
		j, err = journal.NewSQLite(cfg.Journal.DBPath)
	}
	if err != nil {
		return fmt.Errorf("create journal: %w", err)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       cfg.Account.ID,
		Currency: cfg.Account.Currency,
		Balance:  cfg.Account.Balance,
		Equity:   cfg.Account.Balance,
	}, j)

	err = replay.CSV(ctx, cfg.Replay.CSVFile, engine, replay.Options{
		TickThenEvent: cfg.Replay.TickThenEvent,
	})
	if err != nil {
		return fmt.Errorf("replay error: %w", err)
	}

	if cfg.Replay.CloseAtEnd {
		if err := engine.CloseAll(ctx, "EndOfReplay"); err != nil {
			return fmt.Errorf("close all: %w", err)
		}
	}

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

	return nil
}
