package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	trader "github.com/rustyeddy/trader"
)

func main() {
	instrument := flag.String("instrument", "EURUSD", "Instrument to generate data for")
	year := flag.Int("year", 2025, "Year to generate data for")
	timeframe := flag.String("timeframe", "H1", "Timeframe (M1, H1, D1)")
	output := flag.String("output", "testdata", "Output directory for CSV files")
	verbose := flag.Bool("v", false, "Verbose output")

	flag.Parse()

	// Parse timeframe
	var tf trader.Timeframe
	switch *timeframe {
	case "M1":
		tf = trader.M1
	case "H1":
		tf = trader.H1
	case "D1":
		tf = trader.D1
	default:
		log.Fatalf("Invalid timeframe: %s", *timeframe)
	}

	if *verbose {
		fmt.Printf("Generating 12 months of %s %s candles...\n", *instrument, *timeframe)
	}

	// Create output directory
	if err := os.MkdirAll(*output, 0o755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Use the helper from trader package
	paths, err := trader.GenerateSyntheticYearTestData(*output, *instrument, *year, tf)
	if err != nil {
		log.Fatalf("Failed to generate candles: %v", err)
	}

	fmt.Printf("✓ Successfully generated %d months of %s %s data\n", len(paths), *instrument, *timeframe)
	var totalBytes int64
	for i, path := range paths {
		info, _ := os.Stat(path)
		totalBytes += info.Size()
		if *verbose {
			fmt.Printf("  Month %2d: %s (%d bytes)\n", i+1, path, info.Size())
		}
	}
	fmt.Printf("  Total: %d KB\n", totalBytes/1024)
	fmt.Printf("  Output: %s\n", *output)
}
