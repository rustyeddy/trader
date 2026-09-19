package rootcmd

import (
	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/trader/internal/version"
)

// newVersionCmd builds "trader version" (issue #389): a full,
// multi-line report (version.Info.Report) alongside the compact form
// Cobra's own --version/-v flag already prints via cmd.Version. This
// is a small, standalone leaf command (not a family with its own
// subcommands, unlike data/broker/execution/backtest), so it lives
// directly in this package rather than a cmd/trader/version package
// of its own.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print Trader's version and build metadata.",
		Long: "Print Trader's version and build metadata: the version\n" +
			"itself (an exact released tag, a development string derived\n" +
			"from git describe, or \"unknown\"), the commit it was built\n" +
			"from, and that commit's own time, when known (issue #389,\n" +
			"ADR-064). Never fails; unknown fields are simply omitted.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := cmd.OutOrStdout().Write([]byte(version.Current().Report()))
			return err
		},
	}
}
