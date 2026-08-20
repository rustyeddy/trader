package main

import (
	"errors"

	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// newBuildCmd implements "trader data build INSTRUMENT INTERVAL --from
// --to [--format]" (issue #110, formatting added by #111): the
// mutating Build use case. Build publishes canonical data from
// whatever raw data already exists; it never acquires raw data itself
// (Sync's job).
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
			req, err := resolveDatasetRequest(cmd, args, flags)
			if err != nil {
				return err
			}

			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.Build(cmd.Context(), svc.BuildRequest{DatasetRequest: req})
			if err != nil {
				// See datasync.go's identical comment: a secondary
				// formatting failure is joined onto the original
				// service error, never silently dropped and never
				// masking it.
				if formatErr := formatter.FormatBuild(cmd.OutOrStdout(), resp); formatErr != nil {
					return errors.Join(err, formatErr)
				}
				return err
			}

			return formatter.FormatBuild(cmd.OutOrStdout(), resp)
		},
	}

	addDatasetArgFlags(cmd, &flags)
	return cmd
}
