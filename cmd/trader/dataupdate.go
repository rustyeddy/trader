package main

import (
	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// newUpdateCmd implements "trader data update INSTRUMENT INTERVAL
// --from --to" (issue #110): the higher-level Update use case. Update
// calls Service.Update directly -- it does not, and must not,
// reimplement Plan -> Sync -> Build orchestration itself; that
// composition already lives entirely in service/marketdata's own
// Update (issue #107).
func newUpdateCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "update INSTRUMENT INTERVAL",
		Short: "Bring a dataset current (plan, sync, and build as required).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := resolveDatasetRequest(cmd, args, flags)
			if err != nil {
				return err
			}

			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.Update(cmd.Context(), svc.UpdateRequest{DatasetRequest: req})
			if err != nil {
				// printUpdateProgress, not printUpdateResponse: the
				// latter's "already current" line is success-only and
				// would misrepresent a failed Update (see its own doc
				// comment).
				printUpdateProgress(cmd.OutOrStdout(), resp)
				return err
			}

			printUpdateResponse(cmd.OutOrStdout(), resp)
			return nil
		},
	}

	addDatasetArgFlags(cmd, &flags)
	return cmd
}
