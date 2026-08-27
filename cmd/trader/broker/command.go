package broker

import "github.com/spf13/cobra"

// accountFlags holds the "broker" command group's persistent
// flag values, shared by every subcommand: which account to use and
// how it starts funded. Cobra flag names are chosen for CLI
// readability; readAccountFlags is the one place that reads
// them back off a (possibly leaf) *cobra.Command.
type accountFlags struct {
	startingCash string
	currency     string
	accountID    string
}

// New returns the "broker" command group: thin CLI commands
// over the M3 simulated-broker application service (issue #155,
// M3-12).
//
// Every subcommand builds its own fresh, in-memory simulated Broker
// with exactly one account, from --starting-cash/--currency/
// --account-id (see service.go's buildSimBroker):
// adapters/broker/sim has no persistence layer, so state never
// survives between separate "trader broker ..." invocations. This is
// a deliberate, documented ephemeral inspection/demonstration scope
// (see the M3-12 design discussion on issue #155), not a persistent
// paper-trading session — "trader broker snapshot" run on its own
// always shows a freshly funded, flat account, and an order submitted
// by one invocation is never visible to another.
//
// Unlike cmd/trader/data's New, this group has no PersistentPreRunE building
// shared state: constructing a simulated Broker is cheap, and each
// subcommand needs its own anyway (accounts/snapshot never touch a
// listing or price at all; submit does), so each leaf command's own
// RunE builds what it needs directly rather than sharing one
// context-stashed value the way *marketdata.Manager is shared across
// data's several subcommands.
//
// cancel/replace are deliberately not exposed as normal-looking
// commands here: doing so would require secretly submitting a
// synthetic order first to have something to act on, smuggling a
// Submit -> Cancel/Replace business workflow into Cobra itself rather
// than keeping each command a thin transport over one service/broker
// use case. That full path is already covered by service/broker's own
// tests (issue #154); this package's own integration test exercises
// Submit through the real command tree.
func New() *cobra.Command {
	var flags accountFlags

	cmd := &cobra.Command{
		Use:   "broker",
		Short: "Simulated broker account inspection and order submission.",
		Long: "Simulated broker account inspection and order submission.\n\n" +
			"Every invocation builds a fresh, in-memory simulated broker; " +
			"state does not persist between separate trader commands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().StringVar(&flags.startingCash, "starting-cash", "",
		"starting account cash amount (default 10000)")
	cmd.PersistentFlags().StringVar(&flags.currency, "currency", "",
		"account currency (default USD)")
	cmd.PersistentFlags().StringVar(&flags.accountID, "account-id", "",
		"account id to use (default: freshly generated)")

	cmd.AddCommand(newAccountsCmd())
	cmd.AddCommand(newSnapshotCmd())
	cmd.AddCommand(newSubmitCmd())

	return cmd
}
