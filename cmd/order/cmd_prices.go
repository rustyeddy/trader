package order

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/brokers/oanda"
	trader "github.com/rustyeddy/trader"
)

func pricesCmd() *cobra.Command {
	var (
		instrumentsCSV string
		units          int64
	)

	cmd := &cobra.Command{
		Use:   "prices",
		Short: "Fetch live bid/ask prices from OANDA for the major pairs",
		Long: `Query OANDA for current bid/ask prices and print a summary table showing
bid, ask, mid, spread in pips, and USD pip value per lot.

Defaults to all seven major pairs. Supply --instruments to restrict the list.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			svc, err := buildService(ctx)
			if err != nil {
				return err
			}

			names := trader.Majors
			if instrumentsCSV != "" {
				names = splitInstrumentCSV(instrumentsCSV)
			}

			oandaNames := make([]string, 0, len(names))
			for _, name := range names {
				inst := trader.GetInstrument(name)
				if inst == nil {
					return fmt.Errorf("unknown instrument: %s", name)
				}
				oandaNames = append(oandaNames, inst.BaseCurrency+"_"+inst.QuoteCurrency)
			}

			prices, err := svc.OANDA.GetPricing(ctx, svc.AccountID, oandaNames...)
			if err != nil {
				return fmt.Errorf("fetch prices: %w", err)
			}

			printPrices(prices, units, env)
			return nil
		},
	}

	cmd.Flags().StringVar(&instrumentsCSV, "instruments", "", "Comma-separated pairs to query (default: all majors)")
	cmd.Flags().Int64Var(&units, "units", 100_000, "Lot size for pip value column (100000 = 1 standard lot)")
	addCommonFlags(cmd)
	return cmd
}

func printPrices(prices []oanda.Price, units int64, envName string) {
	ts := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	fmt.Printf("\nLive prices — %s — %s\n\n", strings.ToUpper(envName), ts)
	fmt.Printf("%-10s  %10s  %10s  %10s  %10s  %12s\n",
		"Instrument", "Bid", "Ask", "Mid", "Spread", "Pip Value")
	fmt.Printf("%-10s  %10s  %10s  %10s  %10s  %12s\n",
		"──────────", "──────────", "──────────", "──────────", "──────────", "────────────")

	for _, p := range prices {
		traderName := strings.ReplaceAll(p.Instrument, "_", "")
		inst := trader.GetInstrument(traderName)
		if inst == nil {
			fmt.Printf("%-10s  %10.5f  %10.5f  %10.5f\n", traderName, p.Bid, p.Ask, p.Mid)
			continue
		}

		spreadPips := (p.Ask - p.Bid) / inst.PipSize()
		pipVal := inst.PipValueUSD(p.Mid, units, 1)
		dec := pipDecimals(inst)

		fmt.Printf("%-10s  %10.*f  %10.*f  %10.*f  %9.1f p  %12s\n",
			traderName,
			dec, p.Bid,
			dec, p.Ask,
			dec, p.Mid,
			spreadPips,
			fmtPipVal(pipVal),
		)
	}
	fmt.Println()
}

// pipDecimals returns the number of decimal places to display for an instrument:
// one extra digit beyond the pip location (e.g. EURUSD pip=-4 → 5 digits).
func pipDecimals(inst *trader.Instrument) int {
	return max(-inst.PipLocation+1, 2)
}

func fmtPipVal(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

func splitInstrumentCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = trader.NormalizeInstrument(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
