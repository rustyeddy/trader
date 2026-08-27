package execution

import (
	"github.com/spf13/cobra"
)

// newEvaluateCmd implements "trader execution evaluate ..." (issue
// #187, M4-12): the read-only Evaluate use case. It never mutates the
// broker — see New's own doc comment — so it uses noPriceSource
// unconditionally, the same way cmd/trader/broker's accounts/snapshot
// commands do for their own never-submits operations.
func newEvaluateCmd() *cobra.Command {
	var accFlags accountFlags
	var listingFlags simListingFlags
	var intentFl intentFlags
	var format string

	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Size, plan, and risk-evaluate an intent without submitting it.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			formatter, err := resolveFormatter(format)
			if err != nil {
				return err
			}

			accFlags, err = readAccountFlags(cmd)
			if err != nil {
				return err
			}

			svc, req, err := prepareRequest(cmd, accFlags, listingFlags, intentFl, noPriceSource{})
			if err != nil {
				return err
			}

			resp, err := svc.Evaluate(cmd.Context(), req)
			if err != nil && !isRejected(err) {
				return err
			}
			return formatter.FormatEvaluate(cmd.OutOrStdout(), resp)
		},
	}

	registerIntentFlags(cmd, &listingFlags, &intentFl)
	cmd.Flags().StringVar(&format, "format", formatTable, "output format: table or json")
	return cmd
}
