package signalreplay

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func cmdReport() *cobra.Command {
	var reportsDir, signalsPath, out string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Join closed signalreplay trades back to the sweep CSV into an outcome dataset",
		Long: `report reads every backtest JSON report in --reports, keeps only trades
opened by the signalreplay strategy, and joins each closed trade back to the
sweep CSV row that produced its entry signal (by instrument + signal date).
The result is one CSV row per closed trade with R-multiple, hold time, and
the sweep's feature columns attached — the dataset the composite grading
system is fitted against.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if reportsDir == "" {
				return fmt.Errorf("signalreplay report: --reports is required")
			}
			if signalsPath == "" {
				return fmt.Errorf("signalreplay report: --signals is required")
			}
			if out == "" {
				return fmt.Errorf("signalreplay report: --out is required")
			}

			rows, err := BuildOutcomeRows(reportsDir, signalsPath)
			if err != nil {
				return err
			}

			f, err := os.Create(out)
			if err != nil {
				return fmt.Errorf("signalreplay report: create %q: %w", out, err)
			}
			defer f.Close()
			if err := WriteOutcomeCSV(f, rows); err != nil {
				return fmt.Errorf("signalreplay report: write %q: %w", out, err)
			}

			PrintOutcomeSummary(cmd.OutOrStdout(), Summarize(rows))
			fmt.Fprintf(cmd.OutOrStdout(), "\nwrote %s (%d trade%s)\n", out, len(rows), plural(len(rows)))
			return nil
		},
	}
	cmd.Flags().StringVar(&reportsDir, "reports", "", "Directory of backtest JSON reports (required)")
	cmd.Flags().StringVar(&signalsPath, "signals", "", "Path to the review sweep CSV used to generate the config (required)")
	cmd.Flags().StringVar(&out, "out", "", "Output path for the outcome CSV (required)")
	return cmd
}
