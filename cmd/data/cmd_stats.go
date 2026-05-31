package data

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	trader "github.com/rustyeddy/trader"
)

func newStatsCmd(_ *trader.RootConfig) *cobra.Command {
	var (
		instrument string
		timeframe  string
		fromStr    string
		toStr      string
		source     string
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print statistics for a historical candle dataset",
		Long: `Walk a candle dataset and report:
  - Swing:   high-low range distribution in pips
  - Spread:  average spread distribution in pips
  - Trend:   body/range ratio (trending vs consolidating bars)
  - Session: average range and candle count by UTC hour

--from and --to are inclusive dates in YYYY-MM-DD format.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := trader.NormalizeInstrument(instrument)
			if inst == "" {
				return fmt.Errorf("blank --instrument")
			}
			instMeta := trader.GetInstrument(inst)
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

			tr, err := trader.ParseTimeRange(
				from.Format("2006-01-02"),
				toExcl.Format("2006-01-02"),
				timeframe,
			)
			if err != nil {
				return fmt.Errorf("bad range: %w", err)
			}

			analyzers := []trader.Analyzer{
				trader.NewSwingAnalyzer(instMeta),
				trader.NewSpreadAnalyzer(instMeta),
				trader.NewTrendAnalyzer(),
				trader.NewSessionAnalyzer(instMeta),
			}

			dm := trader.NewDataManager([]string{inst}, from, toExcl)
			req := trader.CandleRequest{
				Source:     source,
				Instrument: inst,
				Range:      tr,
			}

			ctx := context.Background()
			itr, err := dm.Candles(ctx, req)
			if err != nil {
				return fmt.Errorf("open candles: %w", err)
			}

			if err := trader.RunAnalysis(ctx, itr, analyzers); err != nil {
				return fmt.Errorf("analysis: %w", err)
			}

			printAnalysis(os.Stdout, inst, timeframe, from, to, analyzers)
			return nil
		},
	}

	cmd.Flags().StringVar(&instrument, "instrument", "", "FX pair (e.g. EURUSD)")
	cmd.Flags().StringVar(&timeframe, "timeframe", "H1", "Candle timeframe: M1, H1, or D1")
	cmd.Flags().StringVar(&fromStr, "from", "", "Start date inclusive (YYYY-MM-DD)")
	cmd.Flags().StringVar(&toStr, "to", "", "End date inclusive (YYYY-MM-DD)")
	cmd.Flags().StringVar(&source, "source", "", "Data source override (default: oanda)")

	_ = cmd.MarkFlagRequired("instrument")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

func printAnalysis(w io.Writer, inst, tf string, from, to time.Time, analyzers []trader.Analyzer) {
	header := fmt.Sprintf("%s %s   %s → %s",
		inst, tf,
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
	)
	bar := "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, bar)
	for _, a := range analyzers {
		fmt.Fprintf(w, "\n%s\n", a.Name())
		for _, s := range a.Stats() {
			fmt.Fprintf(w, "  %-26s %s\n", s.Name, s.Value)
		}
	}
	fmt.Fprintln(w)
}
