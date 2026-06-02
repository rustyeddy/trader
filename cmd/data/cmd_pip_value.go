package data

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	trader "github.com/rustyeddy/trader"
)

// defaultRates are approximate mid-market rates for USD-base pairs used when
// live prices are unavailable.
var defaultRates = map[string]float64{
	"USDJPY": 150.00,
	"USDCHF": 0.90,
	"USDCAD": 1.36,
}

// usdBasePairs are the majors whose pip value depends on the current rate.
var usdBasePairs = []string{"USDJPY", "USDCHF", "USDCAD"}

var pipMultipliers = []float64{1, 10, 100, 1000}

func newPipValueCmd(rc *trader.RootConfig) *cobra.Command {
	var (
		units    int64
		ratesCSV string
		auth     = defaultOandaAuth()
	)

	cmd := &cobra.Command{
		Use:   "pip-value",
		Short: "Show USD value of 1/10/100/1000 pips for each major pair",
		Long: `Print the USD value of 1, 10, 100, and 1000 pips for every major FX pair
at the given position size (default: 100,000 units = 1 standard lot).

For USD-base pairs (USDJPY, USDCHF, USDCAD) the pip value depends on the
current exchange rate.  Rates are resolved in this order:
  1. --rates flag (explicit overrides)
  2. Live OANDA prices (when OANDA_TOKEN is set or --token is supplied)
  3. Built-in approximate defaults`,
		RunE: func(cmd *cobra.Command, args []string) error {
			applyGlobalOANDA(cmd, &auth, rc)
			ctx := context.Background()
			rates, live := resolveRates(ctx, ratesCSV, auth)
			printPipValues(units, rates, live)
			return nil
		},
	}

	cmd.Flags().Int64Var(&units, "units", 100_000, "Position size in units (100000 = 1 standard lot)")
	cmd.Flags().StringVar(&ratesCSV, "rates", "", "Explicit rates for USD-base pairs, e.g. USDJPY=152.50,USDCHF=0.88")
	cmd.Flags().StringVar(&auth.token, "token", auth.token, "OANDA API token (for live rate lookup)")
	cmd.Flags().StringVar(&auth.env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&auth.accountID, "account-id", auth.accountID, "OANDA account ID (auto-discovered if omitted)")

	return cmd
}

// resolveRates returns the rates map and whether live prices were used.
// Priority: explicit --rates > live OANDA > built-in defaults.
func resolveRates(ctx context.Context, ratesCSV string, auth oandaAuth) (rates map[string]float64, live bool) {
	rates = make(map[string]float64, len(defaultRates))
	maps.Copy(rates, defaultRates)

	// Try live prices if a token is available and no explicit rates given.
	if ratesCSV == "" && auth.token != "" {
		if prices, err := fetchMidPrices(ctx, auth, usdBasePairs); err == nil && len(prices) > 0 {
			maps.Copy(rates, prices)
			live = true
		}
	}

	// Explicit --rates always win.
	for pair := range strings.SplitSeq(ratesCSV, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) != 2 {
			continue
		}
		inst := trader.NormalizeInstrument(kv[0])
		v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
		if err == nil && v > 0 {
			rates[inst] = v
			live = false // explicit overrides trump "live" label
		}
	}
	return rates, live
}

// buildRates is retained for tests: merges defaultRates with CSV overrides only.
func buildRates(csv string) map[string]float64 {
	rates, _ := resolveRates(context.Background(), csv, oandaAuth{})
	return rates
}

func printPipValues(units int64, rates map[string]float64, live bool) {
	rateLabel := "approximate"
	if live {
		rateLabel = "live"
	}

	fmt.Printf("\nPip values — %s units", fmtUnits(units))
	fmt.Printf("  (USD per N pips)\n\n")
	fmt.Printf("%-10s  %10s  %10s  %10s  %12s\n",
		"Instrument", "1 pip", "10 pips", "100 pips", "1000 pips")
	fmt.Printf("%-10s  %10s  %10s  %10s  %12s\n",
		"──────────", "──────────", "──────────", "──────────", "────────────")

	var notedPairs []string
	for _, name := range trader.Majors {
		inst := trader.GetInstrument(name)
		if inst == nil {
			continue
		}
		rate := rates[name]
		needsRate := inst.QuoteCurrency != "USD"
		if needsRate {
			notedPairs = append(notedPairs, fmt.Sprintf("%s=%.5g", name, rate))
		}
		marker := " "
		if needsRate {
			marker = "†"
		}

		fmt.Printf("%-10s%s", name, marker)
		for _, pips := range pipMultipliers {
			v := inst.PipValueUSD(rate, units, pips)
			fmt.Printf(" %10s", fmtUSD(v))
		}
		fmt.Println()
	}

	if len(notedPairs) > 0 {
		fmt.Printf("\n† %s rate(s): %s\n", rateLabel, strings.Join(notedPairs, ", "))
		if !live {
			fmt.Println("  Set OANDA_TOKEN for live rates, or override with --rates USDJPY=152.50,...")
		}
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
