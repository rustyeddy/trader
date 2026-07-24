// Package live hosts the journal command for streaming OANDA transactions.
// Strategy bots are managed via 'trader bot' instead.
package live

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/config"
	journalpkg "github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/log"
	accountsvc "github.com/rustyeddy/trader/service/account"
	botsvc "github.com/rustyeddy/trader/service/bots"
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "live",
		Short: "Live trading subsystem",
	}
	cmd.AddCommand(newJournalCmd(rc))
	return cmd
}

// notifyContext returns a context that is cancelled on SIGINT / SIGTERM.
func notifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

func newJournalCmd(rc *config.RootConfig) *cobra.Command {
	var (
		accountID    string
		token        string
		env          string
		journalKind  string
		tradesPath   string
		equityPath   string
		backfillFrom int64
	)
	cmd := &cobra.Command{
		Use:   "journal",
		Short: "Subscribe to OANDA transactions and journal closed trades",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := notifyContext(cmd.Context())
			defer cancel()

			// Token: explicit flag > global config > env var.
			tok := token
			if !cmd.Flags().Changed("token") {
				if rc.OANDA.Token != "" {
					tok = rc.OANDA.Token
				} else {
					tok = os.Getenv("OANDA_TOKEN")
				}
			}

			// Account: explicit flag > global config > env var.
			resolvedAccount := accountID
			if !cmd.Flags().Changed("account-id") {
				if rc.OANDA.AccountID != "" {
					resolvedAccount = rc.OANDA.AccountID
				} else {
					resolvedAccount = os.Getenv("OANDA_ACCOUNT_ID")
				}
			}

			client, err := oanda.NewClient(env, tok)
			if err != nil {
				return err
			}
			resolvedID, err := accountsvc.ResolveAccountID(ctx, client, resolvedAccount)
			if err != nil {
				var amb accountsvc.AmbiguousAccountError
				if errors.As(err, &amb) {
					fmt.Println("Multiple accounts found — specify one with --account-id:")
					for _, id := range amb.Accounts {
						fmt.Printf("  %s\n", id)
					}
				}
				return err
			}
			acc, err := accountsvc.Resolve(ctx, resolvedID, client, log.L)
			if err != nil {
				return err
			}

			journal, err := journalpkg.Open(journalpkg.Config{
				Kind:       journalKind,
				TradesPath: tradesPath,
				EquityPath: equityPath,
			})
			if err != nil {
				return err
			}
			defer journal.Close()

			if backfillFrom > 0 {
				fmt.Printf("Backfilling transactions from ID %d...\n", backfillFrom)
			}
			fmt.Printf("Live journal subscribed to %s (journal=%s). Ctrl-C to exit.\n", resolvedID, journalKind)

			lastID, err := acc.RunLiveJournal(ctx, journal, backfillFrom, botsvc.LookupTradeBotID)
			fmt.Printf("Stopped. lastSeenTxID=%d\n", lastID)
			if err != nil {
				return fmt.Errorf("live journal: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID (auto-discovered if omitted)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (falls back to ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&journalKind, "journal", "json", "Journal backend: csv|json")
	cmd.Flags().StringVar(&tradesPath, "trades-file", "live-trades.jsonl", "Path for journal trade records")
	cmd.Flags().StringVar(&equityPath, "equity-file", "live-equity.jsonl", "Path for journal equity records")
	cmd.Flags().Int64Var(&backfillFrom, "backfill-from", 0, "If >0, poll GetTransactions from this ID before starting the stream")
	return cmd
}
