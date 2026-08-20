package main

import (
	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// newBarsCmd implements "trader data bars INSTRUMENT INTERVAL --from
// --to" (issue #109): the read-only Bars use case. It gathers and
// parses CLI arguments, builds the corresponding service request via
// resolveDatasetRequest, invokes Service.Bars, and prints the result --
// no market-data business logic of its own.
func newBarsCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "bars INSTRUMENT INTERVAL",
		Short: "Read canonical historical bars.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := resolveDatasetRequest(cmd, args, flags)
			if err != nil {
				return err
			}

			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.Bars(cmd.Context(), svc.BarsRequest{DatasetRequest: req})
			if err != nil {
				return err
			}

			printBars(cmd.OutOrStdout(), resp)
			return nil
		},
	}

	addDatasetArgFlags(cmd, &flags)
	return cmd
}
