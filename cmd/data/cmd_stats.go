package data

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	trader "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
)

func newStatsCmd(_ *trader.RootConfig) *cobra.Command {
	var (
		instrument string
		timeframe  string
		fromStr    string
		toStr      string
		source     string
		units      int64
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print statistics for a historical candle dataset",
		Long: `Walk a candle dataset and report:
  - Swing Range Distribution: high-low range distribution in pips
  - Avg Spread Distribution: AvgSpread distribution in pips
  - Trend Distribution: body/range ratio (trending vs consolidating bars)
  - Session: average range and candle count by UTC hour

--from and --to are inclusive dates in YYYY-MM-DD format.
--units adds a USD column showing the dollar value of each pip measurement.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := market.NormalizeInstrument(instrument)
			if inst == "" {
				return fmt.Errorf("blank --instrument")
			}
			instMeta := market.GetInstrument(inst)
			if instMeta == nil {
				return fmt.Errorf("unknown instrument: %s", inst)
			}

			from, err := time.Parse("2006-01-02", fromStr)
			if err != nil {
				return fmt.Errorf("bad --from %q: %w", fromStr, err)
			}
			to, err := time.Parse("2006-01-02", toStr)
			if err != nil {
				return fmt.Errorf("bad --to %q: %w", toStr, err)
			}
			if !from.Before(to) {
				return fmt.Errorf("--from must be before --to")
			}
			// End is exclusive in TimeRange; add one day to include the --to date.
			toExcl := to.AddDate(0, 0, 1)

			tr, err := market.ParseTimeRange(
				from.Format("2006-01-02"),
				toExcl.Format("2006-01-02"),
				timeframe,
			)
			if err != nil {
				return fmt.Errorf("bad range: %w", err)
			}

			analyzers := []marketdata.Analyzer{
				marketdata.NewSwingAnalyzer(instMeta),
				marketdata.NewSpreadAnalyzer(instMeta),
				marketdata.NewTrendAnalyzer(),
				marketdata.NewSessionAnalyzer(instMeta),
			}

			dm := marketdata.NewDataManager([]string{inst}, from, toExcl)
			req := marketdata.CandleRequest{
				Source:     source,
				Instrument: inst,
				Range:      tr,
			}

			ctx := context.Background()
			itr, err := dm.Candles(ctx, req)
			if err != nil {
				return fmt.Errorf("open candles: %w", err)
			}

			if err := marketdata.RunAnalysis(ctx, itr, analyzers); err != nil {
				return fmt.Errorf("analysis: %w", err)
			}

			// For USD-base pairs (USDJPY etc.) use the same approximate rates as
			// pip-value so the dollar column is always populated.
			rate := defaultRates[inst]
			printAnalysis(os.Stdout, instMeta, inst, timeframe, from, to, analyzers, units, rate)
			return nil
		},
	}

	cmd.Flags().StringVar(&instrument, "instrument", "", "FX pair (e.g. EURUSD)")
	cmd.Flags().StringVar(&timeframe, "timeframe", "H1", "Candle timeframe: M1, H1, or D1")
	cmd.Flags().StringVar(&fromStr, "from", "", "Start date inclusive (YYYY-MM-DD)")
	cmd.Flags().StringVar(&toStr, "to", "", "End date inclusive (YYYY-MM-DD)")
	cmd.Flags().StringVar(&source, "source", "", "Data source override (default: oanda)")
	cmd.Flags().Int64Var(&units, "units", 0, "Show USD value per N units alongside pips (e.g. 100000 for a standard lot); 0 disables")

	_ = cmd.MarkFlagRequired("instrument")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

func printAnalysis(w io.Writer, instMeta *market.Instrument, inst, tf string, from, to time.Time, analyzers []marketdata.Analyzer, units int64, rate float64) {
	header := fmt.Sprintf("%s %s   %s → %s",
		inst, tf,
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
	)
	if units > 0 {
		header += fmt.Sprintf("   (USD at %s)", lotLabel(units))
	}
	bar := "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, bar)
	for _, a := range analyzers {
		fmt.Fprintf(w, "\n%s\n", a.Name())
		for _, s := range a.Stats() {
			line := fmt.Sprintf("  %-26s %s", s.Name, s.Value)
			if units > 0 && s.Pips > 0 {
				usd := instMeta.PipValueUSD(rate, units, s.Pips)
				line += fmt.Sprintf("  ($%.2f)", usd)
			}
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintln(w)
}

func lotLabel(units int64) string {
	switch units {
	case 100_000:
		return "standard lot"
	case 10_000:
		return "mini lot"
	case 1_000:
		return "micro lot"
	default:
		return fmt.Sprintf("%d units", units)
	}
}
