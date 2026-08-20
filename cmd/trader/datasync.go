package main

import (
	"errors"

	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// newSyncCmd implements "trader data sync INSTRUMENT INTERVAL --from
// --to [--format]" (issue #110, formatting added by #111): the
// mutating Sync use case. Sync acquires missing raw data; it never
// builds or publishes canonical data (Build's job).
func newSyncCmd() *cobra.Command {
	var flags datasetArgFlags

	cmd := &cobra.Command{
		Use:   "sync INSTRUMENT INTERVAL",
		Short: "Acquire the raw data required to make a dataset available.",
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
			resp, err := dc.Service.Sync(cmd.Context(), svc.SyncRequest{DatasetRequest: req})
			if err != nil {
				// Best-effort: still surface whatever partial progress
				// resp carries (SyncResponse's own contract), but the
				// original service error is always what's returned --
				// a secondary formatting failure is joined onto it,
				// never silently dropped and never masking it.
				if formatErr := formatter.FormatSync(cmd.OutOrStdout(), resp); formatErr != nil {
					return errors.Join(err, formatErr)
				}
				return err
			}

			return formatter.FormatSync(cmd.OutOrStdout(), resp)
		},
	}

	addDatasetArgFlags(cmd, &flags)
	return cmd
}
