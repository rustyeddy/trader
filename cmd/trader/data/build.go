package data

import (
	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// newBuildCmd implements "trader data build INSTRUMENT INTERVAL
// [--from --to] [--format]" (issue #110, formatting added by #111): the
// canonicalize use case, through the same service operation MCP uses
// (CanonicalizeDatasets, issue #439) for one symbol. It publishes
// canonical data from the provider's native data already present; it
// never acquires data itself (Sync's job).
func newBuildCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "build INSTRUMENT INTERVAL",
		Short: "Build and publish canonical data from existing raw data.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			formatter, err := resolveFormatter(flags.format)
			if err != nil {
				return err
			}
			req, err := parseDatasetsRequest(args, flags, true)
			if err != nil {
				return err
			}

			// The same canonicalize operation MCP runs (issue #439), for
			// one symbol. A failed build still prints its partial
			// progress.
			dc, _ := dataContextFrom(cmd.Context())
			res, err := singleResult(dc.Service.CanonicalizeDatasets(cmd.Context(), req, svc.CanonicalizeOptions{}))
			if err != nil {
				return err
			}
			res.Err = instrumentFlagError(res.Err, dc.Provider, flags)
			return formatDatasetResult(cmd.OutOrStdout(), formatter, res)
		},
	}

	addDatasetArgFlags(cmd, &flags)
	optionalRangeUsage(cmd, "the whole raw span")
	return cmd
}
