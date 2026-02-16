package main

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/broker/sim"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/risk"
)

// This example demonstrates proper risk management principles.
// It shows:
// 1. How to calculate position size based on account risk percentage
// 2. How stop distance affects position size
// 3. Comparing different risk percentages
// 4. Currency conversion for different quote currencies

func main() {
	ctx := context.Background()

	j, err := journal.NewCSV("./trades.csv", "./equity.csv")
	if err != nil {
		panic(err)
	}

	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-001",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	engine.Prices().Set(market.Tick{
		Instrument: "EUR_USD",
		Bid:        1.0849,
		Ask:        1.0851,
	})

	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("Starting Equity: $%.2f\n\n", acct.Equity)

	// Demonstrate different risk percentages
	fmt.Println("=== Position Sizing with Different Risk Levels ===")
	fmt.Println()

	meta := market.Instruments["EUR_USD"]
	price, _ := engine.GetTick(ctx, "EUR_USD")
	entryPrice := price.Ask

	riskLevels := []float64{0.005, 0.01, 0.02}         // 0.5%, 1%, 2%
	stopDistances := []float64{0.0010, 0.0020, 0.0030} // 10, 20, 30 pips

	for _, riskPct := range riskLevels {
		fmt.Printf("Risk Level: %.1f%% of equity\n", riskPct*100)
		fmt.Println("Stop Distance | Position Size | Risk Amount")
		fmt.Println("------------- | ------------- | -----------")

		for _, stopDist := range stopDistances {
			stopPrice := entryPrice - stopDist

			size := risk.Calculate(risk.Inputs{
				Equity:         acct.Equity,
				RiskPct:        riskPct,
				EntryPrice:     entryPrice,
				StopPrice:      stopPrice,
				PipLocation:    meta.PipLocation,
				QuoteToAccount: 1.0,
			})

			pips := size.StopPips
			fmt.Printf("%6.0f pips   | %13.0f | $%9.2f\n",
				pips, size.Units, size.RiskAmount)
		}
		fmt.Println()
	}

	// Demonstrate currency conversion for JPY pairs
	fmt.Println("=== Position Sizing with JPY Quote Currency ===")
	fmt.Println()

	engine.Prices().Set(market.Tick{
		Instrument: "USD_JPY",
		Bid:        149.50,
		Ask:        149.52,
	})

	jpyPrice, _ := engine.GetTick(ctx, "USD_JPY")
	jpyMeta := market.Instruments["USD_JPY"]
	jpyEntry := jpyPrice.Ask
	jpyStop := jpyEntry - 0.50 // 50 pips

	// JPY is quote currency, so we need to convert to USD
	quoteToAccount := 1.0 / jpyPrice.Mid()

	jpySize := risk.Calculate(risk.Inputs{
		Equity:         acct.Equity,
		RiskPct:        0.01, // 1% risk
		EntryPrice:     jpyEntry,
		StopPrice:      jpyStop,
		PipLocation:    jpyMeta.PipLocation,
		QuoteToAccount: quoteToAccount,
	})

	fmt.Printf("USD/JPY Position Sizing:\n")
	fmt.Printf("  Entry Price: %.2f\n", jpyEntry)
	fmt.Printf("  Stop Price: %.2f\n", jpyStop)
	fmt.Printf("  Stop Distance: %.1f pips\n", jpySize.StopPips)
	fmt.Printf("  Quote to USD Rate: %.6f\n", quoteToAccount)
	fmt.Printf("  Position Size: %.0f units\n", jpySize.Units)
	fmt.Printf("  Risk Amount: $%.2f\n\n", jpySize.RiskAmount)

	// Demonstrate why larger stops = smaller positions
	fmt.Println("=== Why Stop Distance Matters ===")
	fmt.Println()
	fmt.Println("With the same 1% risk ($1,000), wider stops require smaller positions:")
	fmt.Println()

	fmt.Println("Stop Distance | Units Traded | Why?")
	fmt.Println("------------- | ------------ | ----")

	stopExamples := []struct {
		pips   float64
		reason string
	}{
		{10, "Small stop = more units (concentrated risk per pip)"},
		{20, "Medium stop = medium units (balanced)"},
		{50, "Wide stop = fewer units (risk spread across many pips)"},
	}

	for _, ex := range stopExamples {
		stopPrice := entryPrice - (ex.pips * 0.0001)
		size := risk.Calculate(risk.Inputs{
			Equity:         acct.Equity,
			RiskPct:        0.01,
			EntryPrice:     entryPrice,
			StopPrice:      stopPrice,
			PipLocation:    meta.PipLocation,
			QuoteToAccount: 1.0,
		})
		fmt.Printf("%6.0f pips   | %12.0f | %s\n", ex.pips, size.Units, ex.reason)
	}

	fmt.Println("\n=== Key Takeaways ===")
	fmt.Println("1. Always risk a consistent percentage of your equity (0.5-2%)")
	fmt.Println("2. Wider stops = smaller position sizes for the same risk")
	fmt.Println("3. Account for currency conversion when quote != account currency")
	fmt.Println("4. Never risk more than you can afford to lose")
	fmt.Println("5. Position sizing ensures each trade risks the same dollar amount")
}
