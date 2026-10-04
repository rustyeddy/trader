package backtest

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/internal/backtestcfg"
	"github.com/rustyeddy/trader/internal/id"
	svcbacktest "github.com/rustyeddy/trader/internal/service/backtest"
)

// showFlags holds "trader backtest show"'s own flag values.
type showFlags struct {
	outputDir string
	format    string
}

func newShowCmd() *cobra.Command {
	var flags showFlags

	cmd := &cobra.Command{
		Use:   "show <run-id>",
		Short: "Render a previously run backtest's persisted result.",
		Long: "Render a persisted run snapshot written by a prior 'trader backtest run'\n" +
			"invocation. This performs no backtest orchestration and no metric\n" +
			"recomputation: it reads the exact report.BacktestReport 'run' already\n" +
			"computed and projected, and only renders it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showBacktest(cmd, args[0], flags)
		},
	}

	cmd.Flags().StringVar(&flags.outputDir, "output-dir", "", "directory the run's snapshot was written to (default: TRADER_BACKTEST_OUTPUT_DIR, or "+backtestcfg.DefaultOutputDir+")")
	cmd.Flags().StringVar(&flags.format, "format", formatTable, "output format: "+formatTable+", "+formatJSON+", or "+formatOrg)

	return cmd
}

func showBacktest(cmd *cobra.Command, runIDArg string, flags showFlags) error {
	runID, err := id.ParseRunID(runIDArg)
	if err != nil {
		return fmt.Errorf("invalid run id %q: %w", runIDArg, err)
	}

	outputDir, err := backtestcfg.LoadOutputDir(os.Environ(), "", changedFlag(cmd, "output-dir", flags.outputDir))
	if err != nil {
		return err
	}
	rep, err := svcbacktest.NewRunStore(outputDir).Load(runID)
	if err != nil {
		return err
	}
	return render(cmd.OutOrStdout(), flags.format, rep)
}
