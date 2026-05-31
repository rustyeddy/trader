package data

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	trader "github.com/rustyeddy/trader"
)

// defaultRates are approximate mid-market rates for USD-base pairs used when
// the user does not supply --rates.  Override with e.g. --rates USDJPY=152.50
var defaultRates = map[string]float64{
	"USDJPY": 150.00,
	"USDCHF": 0.90,
	"USDCAD": 1.36,
}

var pipMultipliers = []float64{1, 10, 100, 1000}

func newPipValueCmd(_ *trader.RootConfig) *cobra.Command {
	var (
		units    int64
		ratesCSV string
	)

	cmd := &cobra.Command{
		Use:   "pip-value",
		Short: "Show USD value of 1/10/100/1000 pips for each major pair",
		Long: `Print the USD value of 1, 10, 100, and 1000 pips for every major FX pair
at the given position size (default: 100,000 units = 1 standard lot).

For USD-base pairs (USDJPY, USDCHF, USDCAD) the pip value depends on the
current exchange rate.  Approximate defaults are used unless you supply
--rates with live values.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rates := buildRates(ratesCSV)
			printPipValues(units, rates)
			return nil
		},
	}

	cmd.Flags().Int64Var(&units, "units", 100_000, "Position size in units (100000 = 1 standard lot)")
	cmd.Flags().StringVar(&ratesCSV, "rates", "", "Current rates for USD-base pairs, e.g. USDJPY=152.50,USDCHF=0.88,USDCAD=1.38")

	return cmd
}

// buildRates merges defaultRates with any user-supplied overrides.
func buildRates(csv string) map[string]float64 {
	rates := make(map[string]float64, len(defaultRates))
	maps.Copy(rates, defaultRates)
	if csv == "" {
		return rates
	}
	for pair := range strings.SplitSeq(csv, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) != 2 {
			continue
		}
		inst := trader.NormalizeInstrument(kv[0])
		v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
		if err == nil && v > 0 {
			rates[inst] = v
		}
	}
	return rates
}

func printPipValues(units int64, rates map[string]float64) {
	// header
	fmt.Printf("\nPip values — %s units", fmtUnits(units))
	fmt.Printf("  (USD per N pips)\n\n")
	fmt.Printf("%-10s  %10s  %10s  %10s  %12s\n",
		"Instrument", "1 pip", "10 pips", "100 pips", "1000 pips")
	fmt.Printf("%-10s  %10s  %10s  %10s  %12s\n",
		"──────────", "──────────", "──────────", "──────────", "────────────")

	var approxPairs []string

	for _, name := range trader.Majors {
		inst := trader.GetInstrument(name)
		if inst == nil {
			continue
		}
		rate := rates[name]
		approx := inst.QuoteCurrency != "USD"
		if approx {
			approxPairs = append(approxPairs, fmt.Sprintf("%s=%.4g", name, rate))
		}
		marker := " "
		if approx {
			marker = "†"
		}

		fmt.Printf("%-10s%s", name, marker)
		for _, pips := range pipMultipliers {
			v := inst.PipValueUSD(rate, units, pips)
			fmt.Printf(" %10s", fmtUSD(v))
		}
		fmt.Println()
	}

	if len(approxPairs) > 0 {
		fmt.Printf("\n† approximate rate(s): %s\n", strings.Join(approxPairs, ", "))
		fmt.Println("  Override with --rates USDJPY=152.50,USDCHF=0.88,USDCAD=1.38")
	}
	fmt.Println()
}

func fmtUSD(v float64) string {
	if v >= 1000 {
		return "$" + commaInt(int64(v+0.5))
	}
	if v >= 10 {
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("$%.4f", v)
}

// commaInt formats n with thousands separators (e.g. 10000 → "10,000").
func commaInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+4)
	for i, c := range s {
		pos := len(s) - i
		if i > 0 && pos%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}

func fmtUnits(u int64) string {
	switch u {
	case 100_000:
		return "100,000 (standard lot)"
	case 10_000:
		return "10,000 (mini lot)"
	case 1_000:
		return "1,000 (micro lot)"
	default:
		return fmt.Sprintf("%d", u)
	}
}
