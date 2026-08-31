package backtest

import "github.com/spf13/cobra"

// New returns the "backtest" command group: a thin CLI over the M5
// backtest application service (issue #222, M5-14). See the package
// doc comment for its provisional demo-strategy and persisted-
// snapshot design.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backtest",
		Short: "Run backtests and inspect their results.",
		Long: "Run backtests over the M5 backtest application service.\n\n" +
			"'run' executes a backtest and persists/renders its result;\n" +
			"'show' renders a previously run backtest's persisted result\n" +
			"without recomputing anything.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newShowCmd())

	return cmd
}
