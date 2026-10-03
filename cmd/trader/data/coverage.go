package data

import (
	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// newCoverageCmd implements "trader data coverage INSTRUMENT INTERVAL
// --from --to [--format]" (issue #109, formatting added by #111): the
// read-only Coverage use case.
func newCoverageCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "coverage INSTRUMENT INTERVAL",
		Short: "Report canonical/raw coverage and gaps for a dataset.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			formatter, err := resolveFormatter(flags.format)
			if err != nil {
				return err
			}
			req, err := resolveDatasetRequestRange(cmd, args, flags, true)
			if err != nil {
				return err
			}

			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.Coverage(cmd.Context(), svc.CoverageRequest{DatasetRequest: req})
			if err != nil {
				return err
			}

			return formatter.FormatCoverage(cmd.OutOrStdout(), resp)
		},
	}

	addDatasetArgFlags(cmd, &flags)
	cmd.Flags().Lookup("from").Usage = "range start (YYYY-MM-DD or RFC3339); with --to, or omit both for the existing canonical span"
	cmd.Flags().Lookup("to").Usage = "range end (YYYY-MM-DD or RFC3339); with --from, or omit both for the existing canonical span"
	return cmd
}
