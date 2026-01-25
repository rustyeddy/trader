package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/risk"
	"github.com/rustyeddy/trader/sim"
)

// This example demonstrates a simple single trade with stop loss and take profit.
// It shows the basic workflow of:
// 1. Setting up the trading engine
// 2. Setting initial prices
// 3. Calculating position size based on risk
// 4. Opening a trade with stops
// 5. Simulating price movement that triggers the take profit

func main() {
	ctx := context.Background()

	// Create journal for recording trades
	j, err := journal.NewCSV("./trades.csv", "./equity.csv")
	if err != nil {
		panic(err)
	}

	// Initialize engine with starting capital of $100,000
	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-001",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	// Set initial market price for EUR/USD
	initialBid := 1.0849
	initialAsk := 1.0851
	engine.Prices().Set(broker.Price{
		Instrument: "EUR_USD",
		Bid:        initialBid,
		Ask:        initialAsk,
		Time:       time.Now(),
	})

	fmt.Printf("Initial Price - Bid: %.4f, Ask: %.4f\n", initialBid, initialAsk)

	// Get account information
	acct, err := engine.GetAccount(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Starting Equity: $%.2f\n\n", acct.Equity)

	// Get instrument metadata
	meta := market.Instruments["EUR_USD"]
	price, _ := engine.GetPrice(ctx, "EUR_USD")

	// Calculate position size based on risk
	// Risk 1% of equity with a 20 pip stop loss
	stopPrice := price.Ask - 0.0020 // 20 pips below entry
	size := risk.Calculate(risk.Inputs{
		Equity:         acct.Equity,
		RiskPct:        0.01, // Risk 1% of equity
		EntryPrice:     price.Ask,
		StopPrice:      stopPrice,
		PipLocation:    meta.PipLocation,
		QuoteToAccount: 1.0, // USD is quote currency
	})

	fmt.Printf("Position Sizing:\n")
	fmt.Printf("  Risk Amount: $%.2f (1%% of equity)\n", size.RiskAmount)
	fmt.Printf("  Stop Distance: %.1f pips\n", size.StopPips)
	fmt.Printf("  Position Size: %.0f units\n\n", size.Units)

	// Set take profit at 40 pips (2:1 risk/reward ratio)
	targetPrice := price.Ask + 0.0040

	fmt.Printf("Opening BUY trade:\n")
	fmt.Printf("  Entry: %.4f (ask price)\n", price.Ask)
	fmt.Printf("  Stop Loss: %.4f (20 pips)\n", stopPrice)
	fmt.Printf("  Take Profit: %.4f (40 pips)\n\n", targetPrice)

	// Open the trade
	fill, err := engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: "EUR_USD",
		Units:      size.Units,
		StopLoss:   &stopPrice,
		TakeProfit: &targetPrice,
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Trade Opened: ID=%s\n", fill.TradeID)
	fmt.Printf("  Filled at: %.4f\n", fill.Price)
	fmt.Printf("  Units: %.0f\n\n", fill.Units)

	// Simulate price movement - price moves up to hit take profit
	fmt.Println("Simulating price movement...")

	// Price moves up gradually
	newBid := 1.0890
	newAsk := 1.0892

	err = engine.UpdatePrice(broker.Price{
		Instrument: "EUR_USD",
		Bid:        newBid,
		Ask:        newAsk,
		Time:       time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Price Updated - Bid: %.4f, Ask: %.4f\n", newBid, newAsk)

	// Get final account state
	acct, err = engine.GetAccount(ctx)
	if err != nil {
		panic(err)
	}

	fmt.Printf("\nFinal Results:\n")
	fmt.Printf("  Ending Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Profit/Loss: $%.2f\n", acct.Equity-100_000)
	fmt.Printf("\nCheck trades.csv and equity.csv for detailed records.\n")
}
