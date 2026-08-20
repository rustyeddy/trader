package main

import (
	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// newPlanCmd implements "trader data plan INSTRUMENT INTERVAL --from
// --to [--format]" (issue #109, formatting added by #111): the
// read-only Plan use case. Plan never downloads, builds, or publishes
// anything; it only reports what sync/build (#110) would need to do.
func newPlanCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "plan INSTRUMENT INTERVAL",
		Short: "Report the work required to make a dataset available.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			formatter, err := resolveFormatter(flags.format)
			if err != nil {
				return err
			}
			req, err := resolveDatasetRequest(cmd, args, flags)
			if err != nil {
				return err
			}

			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.Plan(cmd.Context(), svc.PlanRequest{DatasetRequest: req})
			if err != nil {
				return err
			}

			return formatter.FormatPlan(cmd.OutOrStdout(), resp)
		},
	}

	addDatasetArgFlags(cmd, &flags)
	return cmd
}
