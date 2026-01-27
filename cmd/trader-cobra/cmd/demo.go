package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/risk"
	"github.com/rustyeddy/trader/sim"
	"github.com/spf13/cobra"
)

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Run example simulations and demos",
	Long: `Run various example trading simulations to learn how the system works.

Available demos:
  basic    - Simple single trade with stop loss and take profit
  risk     - Demonstrates proper position sizing
  simrun   - Simple simulation runner
  multiple - Managing multiple positions simultaneously

Examples:
  trader demo basic
  trader demo risk`,
}

var demoBasicCmd = &cobra.Command{
	Use:   "basic",
	Short: "Run a basic single trade demo",
	Long: `Demonstrates a simple single trade with stop loss and take profit.

Shows the basic workflow of:
  1. Setting up the trading engine
  2. Setting initial prices
  3. Calculating position size based on risk
  4. Opening a trade with stops
  5. Simulating price movement that triggers the take profit`,
	RunE: runDemoBasic,
}

var demoRiskCmd = &cobra.Command{
	Use:   "risk",
	Short: "Run a risk management demo",
	Long: `Demonstrates proper position sizing based on account risk.

Shows how to:
  - Calculate position size to risk a fixed percentage
  - Set appropriate stop losses
  - Handle different instruments with different pip values`,
	RunE: runDemoRisk,
}

var demoSimrunCmd = &cobra.Command{
	Use:   "simrun",
	Short: "Run a simple simulation",
	Long: `Simple simulation runner that opens a trade and moves the market.

Demonstrates:
  - Opening a market order
  - Updating prices
  - Closing trades manually
  - Recording to CSV journal`,
	RunE: runDemoSimrun,
}

func init() {
	rootCmd.AddCommand(demoCmd)
	demoCmd.AddCommand(demoBasicCmd)
	demoCmd.AddCommand(demoRiskCmd)
	demoCmd.AddCommand(demoSimrunCmd)
}

func runDemoBasic(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	fmt.Println("=== Basic Trade Demo ===")
	fmt.Println()

	j, err := journal.NewCSV("./demo-trades.csv", "./demo-equity.csv")
	if err != nil {
		return err
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "DEMO-001",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	initialBid := 1.0849
	initialAsk := 1.0851
	engine.Prices().Set(broker.Price{
		Instrument: "EUR_USD",
		Bid:        initialBid,
		Ask:        initialAsk,
		Time:       time.Now(),
	})

	fmt.Printf("Initial Price - Bid: %.4f, Ask: %.4f\n", initialBid, initialAsk)

	acct, err := engine.GetAccount(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Starting Equity: $%.2f\n\n", acct.Equity)

	meta := market.Instruments["EUR_USD"]
	price, _ := engine.GetPrice(ctx, "EUR_USD")

	stopPrice := price.Ask - 0.0020
	size := risk.Calculate(risk.Inputs{
		Equity:         acct.Equity,
		RiskPct:        0.01,
		EntryPrice:     price.Ask,
		StopPrice:      stopPrice,
		PipLocation:    meta.PipLocation,
		QuoteToAccount: 1.0,
	})

	fmt.Printf("Position Sizing:\n")
	fmt.Printf("  Risk Amount: $%.2f (1%% of equity)\n", size.RiskAmount)
	fmt.Printf("  Stop Distance: %.1f pips\n", size.StopPips)
	fmt.Printf("  Position Size: %.0f units\n", size.Units)
	fmt.Println()

	targetPrice := price.Ask + 0.0040

	fmt.Printf("Opening BUY trade:\n")
	fmt.Printf("  Entry: %.4f (ask price)\n", price.Ask)
	fmt.Printf("  Stop Loss: %.4f (20 pips)\n", stopPrice)
	fmt.Printf("  Take Profit: %.4f (40 pips)\n\n", targetPrice)

	fill, err := engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: "EUR_USD",
		Units:      size.Units,
		StopLoss:   &stopPrice,
		TakeProfit: &targetPrice,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Trade Opened: ID=%s\n", fill.TradeID)
	fmt.Printf("  Filled at: %.4f\n", fill.Price)
	fmt.Printf("  Units: %.0f\n\n", fill.Units)

	fmt.Println("Simulating price movement...")

	newBid := 1.0890
	newAsk := 1.0892

	err = engine.UpdatePrice(broker.Price{
		Instrument: "EUR_USD",
		Bid:        newBid,
		Ask:        newAsk,
		Time:       time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		return err
	}

	fmt.Printf("Price Updated - Bid: %.4f, Ask: %.4f\n", newBid, newAsk)

	acct, err = engine.GetAccount(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("\nFinal Results:\n")
	fmt.Printf("  Ending Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Profit/Loss: $%.2f\n", acct.Equity-100_000)
	fmt.Printf("\n✓ Check demo-trades.csv and demo-equity.csv for detailed records.\n")

	return nil
}

func runDemoRisk(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Risk Management Demo ===")
	fmt.Println("Demonstrating position sizing for EUR_USD trade")
	fmt.Println()

	acctEquity := 100_000.0
	riskPct := 0.01 // 1%
	entryPrice := 1.0850
	stopPips := 20.0

	meta := market.Instruments["EUR_USD"]
	pipSize := risk.PipSize(meta.PipLocation)
	stopPrice := entryPrice - (stopPips * pipSize)

	size := risk.Calculate(risk.Inputs{
		Equity:         acctEquity,
		RiskPct:        riskPct,
		EntryPrice:     entryPrice,
		StopPrice:      stopPrice,
		PipLocation:    meta.PipLocation,
		QuoteToAccount: 1.0,
	})

	fmt.Printf("Account Details:\n")
	fmt.Printf("  Equity: $%.2f\n", acctEquity)
	fmt.Printf("  Risk: %.1f%% ($%.2f)\n\n", riskPct*100, size.RiskAmount)

	fmt.Printf("Trade Setup:\n")
	fmt.Printf("  Instrument: EUR_USD\n")
	fmt.Printf("  Entry Price: %.4f\n", entryPrice)
	fmt.Printf("  Stop Loss: %.4f\n", stopPrice)
	fmt.Printf("  Stop Distance: %.1f pips\n", stopPips)
	fmt.Println()

	fmt.Printf("Calculated Position:\n")
	fmt.Printf("  Position Size: %.0f units\n", size.Units)
	fmt.Printf("  Risk per Pip: $%.2f\n", size.RiskAmount/stopPips)
	fmt.Printf("  Maximum Loss: $%.2f (if stop hit)\n\n", size.RiskAmount)

	// Now show USD/JPY for comparison
	fmt.Println("Comparing with USD/JPY (different pip value):")
	fmt.Println()

	metaJPY := market.Instruments["USD_JPY"]
	entryJPY := 149.50
	stopPipsJPY := 20.0
	pipSizeJPY := risk.PipSize(metaJPY.PipLocation)
	stopPriceJPY := entryJPY - (stopPipsJPY * pipSizeJPY)

	sizeJPY := risk.Calculate(risk.Inputs{
		Equity:         acctEquity,
		RiskPct:        riskPct,
		EntryPrice:     entryJPY,
		StopPrice:      stopPriceJPY,
		PipLocation:    metaJPY.PipLocation,
		QuoteToAccount: 1.0 / entryJPY,
	})

	fmt.Printf("USD/JPY Trade Setup:\n")
	fmt.Printf("  Entry Price: %.2f\n", entryJPY)
	fmt.Printf("  Stop Loss: %.2f\n", stopPriceJPY)
	fmt.Printf("  Stop Distance: %.1f pips\n", stopPipsJPY)
	fmt.Println()

	fmt.Printf("Calculated Position:\n")
	fmt.Printf("  Position Size: %.0f units\n", sizeJPY.Units)
	fmt.Printf("  Risk per Pip: $%.2f\n", sizeJPY.RiskAmount/stopPipsJPY)
	fmt.Printf("  Maximum Loss: $%.2f (if stop hit)\n\n", sizeJPY.RiskAmount)

	fmt.Println("✓ Notice how position size changes based on pip value!")

	return nil
}

func runDemoSimrun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	fmt.Println("=== Simple Simulation Demo ===")
	fmt.Println()

	j, err := journal.NewCSV("./simrun-trades.csv", "./simrun-equity.csv")
	if err != nil {
		return err
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "SIMRUN-001",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	engine.Prices().Set(broker.Price{
		Instrument: "EUR_USD",
		Bid:        1.0849,
		Ask:        1.0851,
		Time:       time.Now(),
	})

	fill, err := engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: "EUR_USD",
		Units:      10_000,
	})
	if err != nil {
		return err
	}
	fmt.Printf("OPEN  trade=%s units=%.0f entry=%.5f\n", fill.TradeID, fill.Units, fill.Price)

	t2 := time.Now().Add(10 * time.Second)
	if err := engine.UpdatePrice(broker.Price{
		Instrument: "EUR_USD",
		Bid:        1.0860,
		Ask:        1.0862,
		Time:       t2,
	}); err != nil {
		return err
	}

	if err := engine.CloseTrade(ctx, fill.TradeID, "ManualClose"); err != nil {
		return err
	}

	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("CLOSE balance=%.2f equity=%.2f\n", acct.Balance, acct.Equity)
	fmt.Printf("\n✓ Check simrun-trades.csv and simrun-equity.csv for records.\n")

	return nil
}
