package execution

import (
	"github.com/spf13/cobra"
)

// submitFlags holds "trader execution submit"'s own local flags,
// beyond the shared account/listing/intent flags: the fill price a
// resulting Market order should use — this CLI has no live market
// data of its own, matching cmd/trader/broker's own submit command.
type submitFlags struct {
	price string
}

// newSubmitCmd implements "trader execution submit ..." (issue #187,
// M4-12): the mutating Submit use case. It runs the identical
// preparation newEvaluateCmd does, then actually submits the prepared
// request — see New's own doc comment.
func newSubmitCmd() *cobra.Command {
	var accFlags accountFlags
	var listingFlags simListingFlags
	var intentFl intentFlags
	var submitFl submitFlags
	var format string

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Size, plan, risk-evaluate, and submit an intent to the simulated broker.",
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

			prices, err := resolveSubmitPriceSource(normalizeSymbol(listingFlags.symbol), submitFl.price)
			if err != nil {
				return err
			}

			svc, req, err := prepareRequest(cmd, accFlags, listingFlags, intentFl, prices)
			if err != nil {
				return err
			}

			resp, err := svc.Submit(cmd.Context(), req)
			if err != nil && !isRejected(err) {
				return err
			}
			return formatter.FormatSubmit(cmd.OutOrStdout(), resp)
		},
	}

	registerIntentFlags(cmd, &listingFlags, &intentFl)
	cmd.Flags().StringVar(&submitFl.price, "price", "", "fill price for the resulting market order (required)")
	cmd.Flags().StringVar(&format, "format", formatTable, "output format: table or json")
	return cmd
}
