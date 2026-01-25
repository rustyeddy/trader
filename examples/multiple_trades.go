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

// This example demonstrates managing multiple positions simultaneously.
// It shows:
// 1. Opening multiple trades on the same instrument
// 2. Opening trades on different instruments
// 3. Watching positions get stopped out or hit targets
// 4. How margin is tracked across multiple positions

func main() {
	ctx := context.Background()

	// Create journal
	j, err := journal.NewCSV("./trades.csv", "./equity.csv")
	if err != nil {
		panic(err)
	}

	// Initialize engine
	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-001",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	// Set initial prices for both instruments
	engine.Prices().Set(broker.Price{
		Instrument: "EUR_USD",
		Bid:        1.0849,
		Ask:        1.0851,
		Time:       time.Now(),
	})

	engine.Prices().Set(broker.Price{
		Instrument: "USD_JPY",
		Bid:        149.50,
		Ask:        149.52,
		Time:       time.Now(),
	})

	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("Starting Equity: $%.2f\n\n", acct.Equity)

	// Trade 1: BUY EUR/USD
	fmt.Println("=== Opening Trade 1: BUY EUR/USD ===")
	openTrade(ctx, engine, "EUR_USD", true)

	// Trade 2: BUY USD/JPY
	fmt.Println("\n=== Opening Trade 2: BUY USD/JPY ===")
	openTrade(ctx, engine, "USD_JPY", true)

	// Check account state
	acct, _ = engine.GetAccount(ctx)
	fmt.Printf("\nAccount Status:\n")
	fmt.Printf("  Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Margin Used: $%.2f\n", acct.MarginUsed)
	fmt.Printf("  Free Margin: $%.2f\n", acct.FreeMargin)
	fmt.Printf("  Margin Level: %.2f%%\n\n", acct.MarginLevel*100)

	// Simulate price movements
	fmt.Println("=== Simulating Price Movements ===")

	// EUR/USD moves up (profitable)
	fmt.Println("\nEUR/USD moves up...")
	err = engine.UpdatePrice(broker.Price{
		Instrument: "EUR_USD",
		Bid:        1.0890,
		Ask:        1.0892,
		Time:       time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		panic(err)
	}

	// USD/JPY moves down slightly (small loss)
	fmt.Println("USD/JPY moves down...")
	err = engine.UpdatePrice(broker.Price{
		Instrument: "USD_JPY",
		Bid:        149.30,
		Ask:        149.32,
		Time:       time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		panic(err)
	}

	// Final account state
	acct, _ = engine.GetAccount(ctx)
	fmt.Printf("\nFinal Account Status:\n")
	fmt.Printf("  Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Balance: $%.2f\n", acct.Balance)
	fmt.Printf("  Profit/Loss: $%.2f\n", acct.Equity-100_000)
	fmt.Printf("\nCheck trades.csv and equity.csv for complete trade history.\n")
}

func openTrade(ctx context.Context, engine *sim.Engine, instrument string, isBuy bool) {
	acct, _ := engine.GetAccount(ctx)
	price, _ := engine.GetPrice(ctx, instrument)
	meta := market.Instruments[instrument]

	// Calculate position size (risk 1% per trade)
	var stopPrice float64
	var entryPrice float64

	if isBuy {
		entryPrice = price.Ask
		// Calculate stop based on instrument-specific pip sizing
		pipSize := 0.0001
		if meta.PipLocation == -2 {
			pipSize = 0.01
		}
		stopPrice = entryPrice - (0.0020 / pipSize * pipSize)
	} else {
		entryPrice = price.Bid
		pipSize := 0.0001
		if meta.PipLocation == -2 {
			pipSize = 0.01
		}
		stopPrice = entryPrice + (0.0020 / pipSize * pipSize)
	}

	// Calculate quote to account rate
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
		RiskPct:        0.01, // 1% risk per trade
		EntryPrice:     entryPrice,
		StopPrice:      stopPrice,
		PipLocation:    meta.PipLocation,
		QuoteToAccount: quoteToAccount,
	})

	targetPrice := entryPrice
	if isBuy {
		targetPrice = entryPrice + (entryPrice - stopPrice) * 2 // 2:1 R:R
	} else {
		targetPrice = entryPrice - (stopPrice - entryPrice) * 2
	}

	units := size.Units
	if !isBuy {
		units = -units // Negative for SELL
	}

	fmt.Printf("  Instrument: %s\n", instrument)
	fmt.Printf("  Direction: %s\n", map[bool]string{true: "BUY", false: "SELL"}[isBuy])
	fmt.Printf("  Entry: %.4f\n", entryPrice)
	fmt.Printf("  Stop: %.4f\n", stopPrice)
	fmt.Printf("  Target: %.4f\n", targetPrice)
	fmt.Printf("  Units: %.0f\n", size.Units)
	fmt.Printf("  Risk: $%.2f\n", size.RiskAmount)

	_, err := engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: instrument,
		Units:      units,
		StopLoss:   &stopPrice,
		TakeProfit: &targetPrice,
	})
	if err != nil {
		panic(err)
	}
}
