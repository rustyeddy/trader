package main

import (
	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// newBuildCmd implements "trader data build INSTRUMENT INTERVAL --from
// --to" (issue #110): the mutating Build use case. Build publishes
// canonical data from whatever raw data already exists; it never
// acquires raw data itself (Sync's job).
func newBuildCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "build INSTRUMENT INTERVAL",
		Short: "Build and publish canonical data from existing raw data.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := resolveDatasetRequest(cmd, args, flags)
			if err != nil {
				return err
			}

			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.Build(cmd.Context(), svc.BuildRequest{DatasetRequest: req})
			if err != nil {
				printBuildResult(cmd.OutOrStdout(), resp.Result)
				return err
			}

			printBuildResult(cmd.OutOrStdout(), resp.Result)
			return nil
		},
	}

	addDatasetArgFlags(cmd, &flags)
	return cmd
}
