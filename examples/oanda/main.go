package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rustyeddy/trader/oanda"
)

// This example demonstrates how to download historic candles from your OANDA account.
// It shows how to:
// 1. Create an OANDA client with your access token
// 2. Fetch historical candlestick data with different parameters
// 3. Process and display the candle data

func main() {
	// Get access token from environment variable
	// To use this example, set your OANDA access token:
	//   export OANDA_TOKEN="your-access-token-here"
	token := os.Getenv("OANDA_TOKEN")
	if token == "" {
		log.Fatal("OANDA_TOKEN environment variable is not set.\n" +
			"Get your token from OANDA and set it with:\n" +
			"  export OANDA_TOKEN=\"your-access-token-here\"")
	}

	// Create OANDA client
	// Set practice=true to use the practice/demo environment
	// Set practice=false to use the live trading environment
	client := oanda.NewClient(token, true)

	ctx := context.Background()

	// Example 1: Fetch the last 100 5-minute candles using midpoint prices
	fmt.Println("=== Example 1: Last 100 5-minute candles (midpoint) ===")
	candles1, err := client.GetCandles(ctx, oanda.CandlesRequest{
		Instrument:  "EUR_USD",
		Price:       oanda.MidPrice,
		Granularity: oanda.M5,
		Count:       100,
	})
	if err != nil {
		log.Fatalf("Failed to fetch candles: %v", err)
	}
	fmt.Printf("Fetched %d candles\n", len(candles1))
	if len(candles1) > 0 {
		fmt.Printf("First candle: Time=%s, O=%.4f, H=%.4f, L=%.4f, C=%.4f, V=%.0f\n",
			candles1[0].Time.Format("2006-01-02 15:04:05"),
			candles1[0].Open, candles1[0].High, candles1[0].Low, candles1[0].Close, candles1[0].Volume)
		fmt.Printf("Last candle:  Time=%s, O=%.4f, H=%.4f, L=%.4f, C=%.4f, V=%.0f\n\n",
			candles1[len(candles1)-1].Time.Format("2006-01-02 15:04:05"),
			candles1[len(candles1)-1].Open, candles1[len(candles1)-1].High,
			candles1[len(candles1)-1].Low, candles1[len(candles1)-1].Close, candles1[len(candles1)-1].Volume)
	}

	// Example 2: Fetch 1-hour candles for a specific time range using bid prices
	fmt.Println("=== Example 2: 1-hour candles for specific date range (bid prices) ===")
	// Fetch candles from 7 days ago to now
	from := time.Now().AddDate(0, 0, -7)
	to := time.Now()
	candles2, err := client.GetCandles(ctx, oanda.CandlesRequest{
		Instrument:  "EUR_USD",
		Price:       oanda.BidPrice,
		Granularity: oanda.H1,
		From:        &from,
		To:          &to,
	})
	if err != nil {
		log.Fatalf("Failed to fetch candles: %v", err)
	}
	fmt.Printf("Fetched %d candles from %s to %s\n",
		len(candles2), from.Format("2006-01-02"), to.Format("2006-01-02"))
	if len(candles2) > 0 {
		fmt.Printf("First candle: Time=%s, O=%.4f, H=%.4f, L=%.4f, C=%.4f\n",
			candles2[0].Time.Format("2006-01-02 15:04:05"),
			candles2[0].Open, candles2[0].High, candles2[0].Low, candles2[0].Close)
		fmt.Printf("Last candle:  Time=%s, O=%.4f, H=%.4f, L=%.4f, C=%.4f\n\n",
			candles2[len(candles2)-1].Time.Format("2006-01-02 15:04:05"),
			candles2[len(candles2)-1].Open, candles2[len(candles2)-1].High,
			candles2[len(candles2)-1].Low, candles2[len(candles2)-1].Close)
	}

	// Example 3: Fetch daily candles with ask prices
	fmt.Println("=== Example 3: Last 30 daily candles (ask prices) ===")
	candles3, err := client.GetCandles(ctx, oanda.CandlesRequest{
		Instrument:  "USD_JPY",
		Price:       oanda.AskPrice,
		Granularity: oanda.D,
		Count:       30,
	})
	if err != nil {
		log.Fatalf("Failed to fetch candles: %v", err)
	}
	fmt.Printf("Fetched %d daily candles for USD_JPY\n", len(candles3))
	if len(candles3) > 0 {
		fmt.Printf("First candle: Time=%s, O=%.4f, H=%.4f, L=%.4f, C=%.4f\n",
			candles3[0].Time.Format("2006-01-02"),
			candles3[0].Open, candles3[0].High, candles3[0].Low, candles3[0].Close)
		fmt.Printf("Last candle:  Time=%s, O=%.4f, H=%.4f, L=%.4f, C=%.4f\n\n",
			candles3[len(candles3)-1].Time.Format("2006-01-02"),
			candles3[len(candles3)-1].Open, candles3[len(candles3)-1].High,
			candles3[len(candles3)-1].Low, candles3[len(candles3)-1].Close)
	}

	// Example 4: Show all available granularities
	fmt.Println("=== Available Granularities ===")
	fmt.Println("Seconds: S5, S10, S15, S30")
	fmt.Println("Minutes: M1, M2, M4, M5, M10, M15, M30")
	fmt.Println("Hours:   H1, H2, H3, H4, H6, H8, H12")
	fmt.Println("Days:    D (daily)")
	fmt.Println("Weeks:   W (weekly)")
	fmt.Println("Months:  M (monthly)")
	fmt.Println("\nAvailable Price Components: M (mid), B (bid), A (ask), BA (bid & ask)")
}
