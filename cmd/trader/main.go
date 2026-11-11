package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		slog.Error("backtest failed", "error", err)
		os.Exit(1)
	}
}

// newRootCmd builds the trader command. With no subcommand it backtests every
// scenario and prints one summary each to stdout. Errors are returned to
// main, which logs them, so cobra does not print them a second time.
func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "trader",
		Short:         "Backtest the EMAX scenarios and print a summary of each",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return trader.Run(cmd.OutOrStdout())
		},
	}
}
