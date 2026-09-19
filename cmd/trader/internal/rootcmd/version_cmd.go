package rootcmd

import (
	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/version"
)

// newVersionCmd builds "trader version" (issue #389): a full,
// multi-line report (version.Info.Report) alongside the compact form
// Cobra's own --version/-v flag already prints via cmd.Version. This
// is a small, standalone leaf command (not a family with its own
// subcommands, unlike data/broker/execution/backtest), so it lives
// directly in this package rather than a package of its own; the
// version-resolution logic itself lives in the top-level, importable
// github.com/rustyeddy/trader/version package, not here.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print Trader's version and build metadata.",
		Long: "Print Trader's version and build metadata (issue #389,\n" +
			"ADR-064). Version is resolved with this precedence: an exact\n" +
			"git-tag-derived version (any build made via `make build`/\n" +
			"`make install`); otherwise the Go main module's own version\n" +
			"(a version-qualified `go install .../trader@vX.Y.Z`, or a\n" +
			"local `go build` on a toolchain that infers one); otherwise a\n" +
			"\"devel+<revision>\" placeholder derived from VCS build info\n" +
			"alone; otherwise \"unknown\". The commit and its own commit\n" +
			"time are additionally reported when known. Never fails;\n" +
			"unknown fields are simply omitted. See the version package's\n" +
			"own doc comment for the complete, authoritative precedence.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := cmd.OutOrStdout().Write([]byte(version.Current().Report()))
			return err
		},
	}
}
