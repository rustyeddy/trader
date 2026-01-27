package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/risk"
	"github.com/rustyeddy/trader/sim"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a simulation from a config file",
	Long: `Run a trading simulation using settings from a configuration file.

The config file specifies account parameters, strategy settings, and price steps
to simulate market movement.

Example:
  trader run -config examples/configs/basic.yaml`,
	RunE: runRun,
}

var runConfigPath string

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runConfigPath, "config", "f", "", "path to config file (YAML or JSON) (required)")
	runCmd.MarkFlagRequired("config")
}

func runRun(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadFromFile(runConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Printf("Running simulation with config: %s\n", runConfigPath)
	fmt.Printf("  Account: %s (Balance: $%.2f %s)\n", cfg.Account.ID, cfg.Account.Balance, cfg.Account.Currency)
	fmt.Printf("  Strategy: %s (Risk: %.1f%%, Stop: %.0f pips, Target: %.0f pips)\n",
		cfg.Strategy.Instrument, cfg.Strategy.RiskPercent*100, cfg.Strategy.StopPips, cfg.Strategy.TargetPips)
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
		return fmt.Errorf("unknown instrument: %s", cfg.Strategy.Instrument)
	}

	pipSize := risk.PipSize(meta.PipLocation)
	stopPrice := price.Ask - (cfg.Strategy.StopPips * pipSize)

	var quoteToAccount float64
	if meta.QuoteCurrency == "USD" {
		quoteToAccount = 1.0
	} else if meta.QuoteCurrency == "JPY" {
		quoteToAccount = 1.0 / price.Mid()
	} else {
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

	_, err = engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: cfg.Strategy.Instrument,
		Units:      size.Units,
		StopLoss:   &stopPrice,
		TakeProfit: &targetPrice,
	})
	if err != nil {
		return fmt.Errorf("create order: %w", err)
	}

	for i, step := range cfg.Simulation.PriceSteps {
		delay, err := step.ParseDuration()
		if err != nil {
			return fmt.Errorf("invalid delay in step %d: %w", i, err)
		}

		fmt.Printf("Updating price to Bid: %.4f, Ask: %.4f (after %s)\n", step.Bid, step.Ask, delay)
		err = engine.UpdatePrice(broker.Price{
			Instrument: cfg.Strategy.Instrument,
			Bid:        step.Bid,
			Ask:        step.Ask,
			Time:       time.Now().Add(delay),
		})
		if err != nil {
			return fmt.Errorf("update price: %w", err)
		}
	}

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

	return nil
}
