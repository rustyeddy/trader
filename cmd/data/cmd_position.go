package data

import (
	"fmt"
	"math"
	"os"

	"github.com/spf13/cobra"

	trader "github.com/rustyeddy/trader"
)

func newPositionCmd(_ *trader.RootConfig) *cobra.Command {
	var (
		instrument string
		price      float64
		units      int64
		notional   float64
	)

	cmd := &cobra.Command{
		Use:   "position",
		Short: "Convert between position size and USD notional value",
		Long: `Calculate USD notional value and margin required for a position.

With no --units or --notional flag a reference table for micro/mini/standard
lots is printed.  Supply one of the optional flags for a specific calculation:

  --units N      show notional and margin for N units
  --notional N   show how many units are needed for $N notional

Notional = units × price  (for USD-quoted pairs like EURUSD)
         = units           (for USD-base pairs like USDJPY, since base IS USD)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := trader.NormalizeInstrument(instrument)
			instMeta := trader.GetInstrument(inst)
			if instMeta == nil {
				return fmt.Errorf("unknown instrument: %s", instrument)
			}
			if price <= 0 {
				return fmt.Errorf("--price must be a positive number")
			}
			if units < 0 {
				return fmt.Errorf("--units must be non-negative")
			}
			if notional < 0 {
				return fmt.Errorf("--notional must be non-negative")
			}

			switch {
			case units > 0:
				printSinglePosition(instMeta, price, units)
			case notional > 0:
				printUnitsForNotional(instMeta, price, notional)
			default:
				printPositionTable(instMeta, price)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&instrument, "instrument", "", "FX pair (e.g. EURUSD)")
	cmd.Flags().Float64Var(&price, "price", 0, "Current mid price for the pair (required)")
	cmd.Flags().Int64Var(&units, "units", 0, "Show notional and margin for this position size")
	cmd.Flags().Float64Var(&notional, "notional", 0, "Show units needed for this USD notional amount")

	_ = cmd.MarkFlagRequired("instrument")
	_ = cmd.MarkFlagRequired("price")

	return cmd
}

// notionalUSD returns the USD value of a position of the given size.
func notionalUSD(inst *trader.Instrument, midPrice float64, units int64) float64 {
	if inst.BaseCurrency == "USD" {
		return float64(units)
	}
	return float64(units) * midPrice
}

// unitsForNotional returns the number of units needed to achieve targetUSD notional.
func unitsForNotional(inst *trader.Instrument, midPrice float64, targetUSD float64) int64 {
	if inst.BaseCurrency == "USD" {
		return int64(math.Round(targetUSD))
	}
	if midPrice <= 0 {
		return 0
	}
	return int64(math.Round(targetUSD / midPrice))
}

func printPositionTable(inst *trader.Instrument, price float64) {
	marginPct := inst.MarginRate.Float64() * 100

	fmt.Fprintf(os.Stdout, "\nPosition calculator — %s @ %.5f  (margin %.1f%%)\n\n",
		inst.Name, price, marginPct)
	fmt.Fprintf(os.Stdout, "%-18s  %10s  %16s  %14s\n",
		"Lot size", "Units", "Notional (USD)", "Margin req.")
	fmt.Fprintf(os.Stdout, "%-18s  %10s  %16s  %14s\n",
		"──────────────────", "──────────", "────────────────", "──────────────")

	type row struct {
		label string
		units int64
	}
	rows := []row{
		{"micro (0.01)", 1_000},
		{"mini  (0.1)", 10_000},
		{"standard (1.0)", 100_000},
	}
	for _, r := range rows {
		n := notionalUSD(inst, price, r.units)
		m := n * inst.MarginRate.Float64()
		fmt.Fprintf(os.Stdout, "%-18s  %10s  %16s  %14s\n",
			r.label, commaInt(r.units), fmtDollar(n), fmtDollar(m))
	}
	fmt.Fprintln(os.Stdout)
}

func printSinglePosition(inst *trader.Instrument, price float64, units int64) {
	n := notionalUSD(inst, price, units)
	m := n * inst.MarginRate.Float64()
	lots := float64(units) / 100_000

	fmt.Fprintf(os.Stdout, "\n%s units of %s @ %.5f\n", fmtUnits(units), inst.Name, price)
	fmt.Fprintf(os.Stdout, "  Lots         %.2f\n", lots)
	fmt.Fprintf(os.Stdout, "  Notional     %s\n", fmtDollar(n))
	fmt.Fprintf(os.Stdout, "  Margin (%.1f%%)  %s\n", inst.MarginRate.Float64()*100, fmtDollar(m))
	fmt.Fprintln(os.Stdout)
}

func printUnitsForNotional(inst *trader.Instrument, price float64, targetUSD float64) {
	u := unitsForNotional(inst, price, targetUSD)
	lots := float64(u) / 100_000
	actual := notionalUSD(inst, price, u)
	m := actual * inst.MarginRate.Float64()

	fmt.Fprintf(os.Stdout, "\n$%.2f notional of %s @ %.5f\n", targetUSD, inst.Name, price)
	fmt.Fprintf(os.Stdout, "  Units        %s\n", fmtUnits(u))
	fmt.Fprintf(os.Stdout, "  Lots         %.4f\n", lots)
	fmt.Fprintf(os.Stdout, "  Actual notional  %s\n", fmtDollar(actual))
	fmt.Fprintf(os.Stdout, "  Margin (%.1f%%)    %s\n", inst.MarginRate.Float64()*100, fmtDollar(m))
	fmt.Fprintln(os.Stdout)
}

func fmtDollar(v float64) string {
	if v >= 1000 {
		return "$" + commaInt(int64(math.Round(v)))
	}
	return fmt.Sprintf("$%.2f", v)
}
