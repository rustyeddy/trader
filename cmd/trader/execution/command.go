package execution

import "github.com/spf13/cobra"

// accountFlags holds the "execution" command group's persistent
// account flags, shared by every subcommand — the same ephemeral
// simulated-account shape cmd/trader/broker's own accountFlags
// establishes (issue #155/M3-12), duplicated here rather than shared
// across command-family packages (issue #201).
type accountFlags struct {
	startingCash string
	currency     string
	accountID    string
}

// New returns the "execution" command group: thin CLI commands over
// the M4 execution/risk application service (issue #187, M4-12).
//
// Every subcommand builds its own fresh, in-memory simulated Broker
// plus execution.Planner/risk.Engine/risk.Sizer/pipeline.Pipeline
// stack from --starting-cash/--currency/--account-id (see service.go's
// buildService) — the same ephemeral, no-persistence-between-invocations
// scope cmd/trader/broker's own New already documents, for the
// identical reason: adapters/broker/sim has no persistence layer.
//
// evaluate never mutates or submits to the broker: it runs sizing,
// planning, and risk admission and shows the resulting Proposal/
// Decision/approved Request, but never calls broker.Account.Submit
// (#187's own "read-only planning/evaluation commands cause no broker
// writes" acceptance criterion, satisfied by service/execution.Service.
// Evaluate never reaching that call — not by CLI-level discipline).
// Evaluate does still call OpenAccount and Snapshot, the same read
// every use case needs for a fresh, authoritative account state — a
// read, not a write. submit runs the identical preparation and then
// actually submits the prepared request.
func New() *cobra.Command {
	var flags accountFlags

	cmd := &cobra.Command{
		Use:   "execution",
		Short: "Execution/risk pipeline inspection and order submission.",
		Long: "Execution/risk pipeline inspection and order submission.\n\n" +
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

	cmd.AddCommand(newEvaluateCmd())
	cmd.AddCommand(newSubmitCmd())

	return cmd
}
