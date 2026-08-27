package data

import (
	"errors"

	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// newUpdateCmd implements "trader data update INSTRUMENT INTERVAL
// --from --to [--format]" (issue #110, formatting added by #111): the
// higher-level Update use case. Update calls Service.Update directly
// -- it does not, and must not, reimplement Plan -> Sync -> Build
// orchestration itself; that composition already lives entirely in
// service/marketdata's own Update (issue #107).
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
			req, err := resolveDatasetRequest(cmd, args, flags)
			if err != nil {
				return err
			}

			dc, _ := dataContextFrom(cmd.Context())
			resp, err := dc.Service.Update(cmd.Context(), svc.UpdateRequest{DatasetRequest: req})
			if err != nil {
				// FormatUpdateProgress, not FormatUpdate: the latter's
				// "already current" claim is success-only and would
				// misrepresent a failed Update (see Formatter's own
				// doc comment). Same join-don't-mask policy as
				// sync.go/build.go for a secondary formatting
				// failure.
				if formatErr := formatter.FormatUpdateProgress(cmd.OutOrStdout(), resp); formatErr != nil {
					return errors.Join(err, formatErr)
				}
				return err
			}

			return formatter.FormatUpdate(cmd.OutOrStdout(), resp)
		},
	}

	addDatasetArgFlags(cmd, &flags)
	return cmd
}
