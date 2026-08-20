package main

import (
	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// newSyncCmd implements "trader data sync INSTRUMENT INTERVAL --from
// --to" (issue #110): the mutating Sync use case. Sync acquires
// missing raw data; it never builds or publishes canonical data
// (Build's job).
func newSyncCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "sync INSTRUMENT INTERVAL",
		Short: "Acquire the raw data required to make a dataset available.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := resolveDatasetRequest(cmd, args, flags)
			if err != nil {
				return err
			}

			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.Sync(cmd.Context(), svc.SyncRequest{DatasetRequest: req})
			if err != nil {
				printSyncResult(cmd.OutOrStdout(), resp.Result)
				return err
			}

			printSyncResult(cmd.OutOrStdout(), resp.Result)
			return nil
		},
	}

	addDatasetArgFlags(cmd, &flags)
	return cmd
}
