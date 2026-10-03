package data

import (
	"fmt"

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
			req, err := parseDatasetsRequest(args, flags, true)
			if err != nil {
				return err
			}

			// The same coverage operation MCP runs (issue #439), for one
			// symbol.
			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.DatasetsCoverage(cmd.Context(), req)
			if err != nil {
				return err
			}
			if len(resp.Results) != 1 {
				return fmt.Errorf("expected one coverage result, got %d", len(resp.Results))
			}
			if res := resp.Results[0]; res.Err != nil {
				return instrumentFlagError(res.Err, dc.Provider, flags)
			}

			return formatter.FormatCoverage(cmd.OutOrStdout(), svc.CoverageResponse{Coverage: resp.Results[0].Coverage})
		},
	}

	addDatasetArgFlags(cmd, &flags)
	optionalRangeUsage(cmd, "the existing canonical span")
	return cmd
}
