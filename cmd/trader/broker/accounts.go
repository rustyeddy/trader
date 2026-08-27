package broker

import (
	"github.com/spf13/cobra"

	svcbroker "github.com/rustyeddy/trader/service/broker"
)

// newAccountsCmd implements "trader broker accounts [--format]"
// (issue #155, M3-12): lists the accounts a freshly built simulated
// Broker knows about — always exactly the one --starting-cash/
// --currency/--account-id describes, since this package never builds
// a Broker with more than one account. It exists primarily to exercise
// and demonstrate the read-only Accounts use case end to end, the same
// way "trader broker snapshot" demonstrates Snapshot.
func newAccountsCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "accounts",
		Short: "List the simulated broker's accounts.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			formatter, err := resolveFormatter(format)
			if err != nil {
				return err
			}

			flags, err := readAccountFlags(cmd)
			if err != nil {
				return err
			}
			cfg, err := buildAccountConfig(cmd, flags)
			if err != nil {
				return err
			}
			svc, _, err := buildService(cmd, cfg, noPriceSource{})
			if err != nil {
				return err
			}

			resp, err := svc.Accounts(cmd.Context(), svcbroker.AccountsRequest{})
			if err != nil {
				return err
			}
			return formatter.FormatAccounts(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Flags().StringVar(&format, "format", formatTable, "output format: table or json")
	return cmd
}

// newSnapshotCmd implements "trader broker snapshot [--format]"
// (issue #155, M3-12): the read-only Snapshot use case for the one
// freshly built account. Run on its own, it always shows a freshly
// funded, flat account — see New's own doc comment for why.
func newSnapshotCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Show the simulated account's current snapshot.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			formatter, err := resolveFormatter(format)
			if err != nil {
				return err
			}

			flags, err := readAccountFlags(cmd)
			if err != nil {
				return err
			}
			cfg, err := buildAccountConfig(cmd, flags)
			if err != nil {
				return err
			}
			svc, accountID, err := buildService(cmd, cfg, noPriceSource{})
			if err != nil {
				return err
			}

			resp, err := svc.Snapshot(cmd.Context(), svcbroker.SnapshotRequest{AccountRequest: svcbroker.AccountRequest{AccountID: accountID}})
			if err != nil {
				return err
			}
			return formatter.FormatSnapshot(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Flags().StringVar(&format, "format", formatTable, "output format: table or json")
	return cmd
}
