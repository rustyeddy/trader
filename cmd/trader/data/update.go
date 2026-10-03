package data

import (
	"github.com/spf13/cobra"
)

// newUpdateCmd implements "trader data update INSTRUMENT INTERVAL
// [--from --to] [--format]" (issue #110, formatting added by #111): the
// update use case, through the same service operation MCP uses
// (UpdateDatasets, issue #439) for one symbol. It does not, and must
// not, reimplement Plan -> Sync -> Build orchestration itself; that
// composition lives entirely in service/marketdata.
func newUpdateCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "update INSTRUMENT INTERVAL",
		Short: "Bring a dataset current (plan, sync, and build as required).",
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

			// The same update operation MCP runs (issue #439), for one
			// symbol. A failed update prints only its partial progress
			// (FormatUpdateProgress), never FormatUpdate's success-only
			// "already current" claim.
			dc, _ := dataContextFrom(cmd.Context())
			res, err := singleResult(dc.Service.UpdateDatasets(cmd.Context(), req))
			if err != nil {
				return err
			}
			res.Err = instrumentFlagError(res.Err, dc.Provider, flags)
			return formatDatasetResult(cmd.OutOrStdout(), formatter, res)
		},
	}

	addDatasetArgFlags(cmd, &flags)
	optionalRangeUsage(cmd, "from the last canonical bar through now")
	return cmd
}
