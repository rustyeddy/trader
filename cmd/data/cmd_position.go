package data

import (
	"context"
	"fmt"
	"math"
	"os"

	"github.com/spf13/cobra"

	trader "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/market"
)

func newPositionCmd(rc *trader.RootConfig) *cobra.Command {
	var (
		instrument string
		price      float64
		units      int64
		notional   float64
		pips       float64
		auth       = defaultOandaAuth()
	)

	cmd := &cobra.Command{
		Use:   "position",
		Short: "Convert between position size, USD notional value, and pip P&L",
		Long: `Calculate USD notional value, margin required, and pip P&L for a position.

If --price is omitted the current mid price is fetched from OANDA
(requires OANDA_TOKEN in the environment or --token).

Without --units or --notional a reference table for micro/mini/standard
lots is printed.  Supply one of those flags for a specific calculation:

  --units N      show notional, margin, and pip P&L for N units
  --notional N   show how many units are needed for $N notional
  --pips N       add a column showing the USD value of N pips

Notional = units × price  (USD-quoted pairs: EURUSD, GBPUSD, AUDUSD, NZDUSD)
         = units           (USD-base pairs:   USDJPY, USDCHF, USDCAD)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			applyGlobalOANDA(cmd, &auth, rc)
			inst := market.NormalizeInstrument(instrument)
			instMeta := market.GetInstrument(inst)
			if instMeta == nil {
				return fmt.Errorf("unknown instrument: %s", instrument)
			}

			ctx := context.Background()
			if price == 0 {
				mid, err := fetchMidPrices(ctx, auth, []string{inst})
				if err != nil {
					return fmt.Errorf("--price not set and live fetch failed: %w", err)
				}
				price = mid[inst]
				if price == 0 {
					return fmt.Errorf("OANDA returned zero price for %s", inst)
				}
			}

			if units < 0 || notional < 0 || pips < 0 {
				return fmt.Errorf("--units, --notional, and --pips must be non-negative")
			}

			switch {
			case units > 0:
				printSinglePosition(instMeta, price, units, pips)
			case notional > 0:
				printUnitsForNotional(instMeta, price, notional, pips)
			default:
				printPositionTable(instMeta, price, pips)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&instrument, "instrument", "", "FX pair (e.g. EURUSD)")
	cmd.Flags().Float64Var(&price, "price", 0, "Current mid price (fetched from OANDA if omitted)")
	cmd.Flags().Int64Var(&units, "units", 0, "Show notional and margin for this position size")
	cmd.Flags().Float64Var(&notional, "notional", 0, "Show units needed for this USD notional amount")
	cmd.Flags().Float64Var(&pips, "pips", 0, "Show USD value of N pips alongside each position size")
	cmd.Flags().StringVar(&auth.token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (for live price lookup)")
	cmd.Flags().StringVar(&auth.env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&auth.accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID (auto-discovered if omitted)")

	_ = cmd.MarkFlagRequired("instrument")

	return cmd
}

// notionalUSD returns the USD value of a position of the given size.
func notionalUSD(inst *market.Instrument, midPrice float64, units int64) float64 {
	if inst.BaseCurrency == "USD" {
		return float64(units)
	}
	return float64(units) * midPrice
}

// unitsForNotional returns the number of units needed to achieve targetUSD notional.
func unitsForNotional(inst *market.Instrument, midPrice float64, targetUSD float64) int64 {
	if inst.BaseCurrency == "USD" {
		return int64(math.Round(targetUSD))
	}
	if midPrice <= 0 {
		return 0
	}
	return int64(math.Round(targetUSD / midPrice))
}

func printPositionTable(inst *market.Instrument, price, pips float64) {
	marginPct := inst.MarginRate.Float64() * 100
	pipsHeader := ""
	if pips > 0 {
		pipsHeader = fmt.Sprintf("  %12s", fmt.Sprintf("%.0f pips", pips))
	}

	fmt.Fprintf(os.Stdout, "\nPosition calculator — %s @ %.5f  (margin %.1f%%)\n\n",
		inst.Name, price, marginPct)
	fmt.Fprintf(os.Stdout, "%-18s  %10s  %16s  %14s%s\n",
		"Lot size", "Units", "Notional (USD)", "Margin req.", pipsHeader)
	fmt.Fprintf(os.Stdout, "%-18s  %10s  %16s  %14s%s\n",
		"──────────────────", "──────────", "────────────────", "──────────────",
		pipsDivider(pips))

	type row struct {
		label string
		units int64
	}
	for _, r := range []row{
		{"micro (0.01)", 1_000},
		{"mini  (0.1)", 10_000},
		{"standard (1.0)", 100_000},
	} {
		n := notionalUSD(inst, price, r.units)
		m := n * inst.MarginRate.Float64()
		pipCol := ""
		if pips > 0 {
			pipCol = fmt.Sprintf("  %12s", fmtDollar(inst.PipValueUSD(price, r.units, pips)))
		}
		fmt.Fprintf(os.Stdout, "%-18s  %10s  %16s  %14s%s\n",
			r.label, commaInt(r.units), fmtDollar(n), fmtDollar(m), pipCol)
	}
	fmt.Fprintln(os.Stdout)
}

func printSinglePosition(inst *market.Instrument, price float64, units int64, pips float64) {
	n := notionalUSD(inst, price, units)
	m := n * inst.MarginRate.Float64()
	lots := float64(units) / 100_000

	fmt.Fprintf(os.Stdout, "\n%s units of %s @ %.5f\n", commaInt(units), inst.Name, price)
	fmt.Fprintf(os.Stdout, "  Lots             %.4f\n", lots)
	fmt.Fprintf(os.Stdout, "  Notional         %s\n", fmtDollar(n))
	fmt.Fprintf(os.Stdout, "  Margin (%.1f%%)    %s\n", inst.MarginRate.Float64()*100, fmtDollar(m))
	if pips > 0 {
		fmt.Fprintf(os.Stdout, "  %.0f pips P&L     %s\n", pips, fmtDollar(inst.PipValueUSD(price, units, pips)))
	}
	fmt.Fprintln(os.Stdout)
}

func printUnitsForNotional(inst *market.Instrument, price float64, targetUSD float64, pips float64) {
	u := unitsForNotional(inst, price, targetUSD)
	lots := float64(u) / 100_000
	actual := notionalUSD(inst, price, u)
	m := actual * inst.MarginRate.Float64()

	fmt.Fprintf(os.Stdout, "\n$%.2f notional of %s @ %.5f\n", targetUSD, inst.Name, price)
	fmt.Fprintf(os.Stdout, "  Units            %s\n", commaInt(u))
	fmt.Fprintf(os.Stdout, "  Lots             %.4f\n", lots)
	fmt.Fprintf(os.Stdout, "  Actual notional  %s\n", fmtDollar(actual))
	fmt.Fprintf(os.Stdout, "  Margin (%.1f%%)    %s\n", inst.MarginRate.Float64()*100, fmtDollar(m))
	if pips > 0 {
		fmt.Fprintf(os.Stdout, "  %.0f pips P&L     %s\n", pips, fmtDollar(inst.PipValueUSD(price, u, pips)))
	}
	fmt.Fprintln(os.Stdout)
}

func pipsDivider(pips float64) string {
	if pips <= 0 {
		return ""
	}
	return "  ────────────"
}

func fmtDollar(v float64) string {
	if v >= 1000 {
		return "$" + commaInt(int64(math.Round(v)))
	}
	return fmt.Sprintf("$%.2f", v)
}
