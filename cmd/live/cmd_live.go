// Package live hosts CLI commands for the live trading subsystem. All
// business logic lives in service; these handlers parse flags, call into
// the service, and format output.
package live

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
)

func New(rc *trader.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "live",
		Short: "Live trading subsystem",
	}
	cmd.AddCommand(newJournalCmd(rc))
	cmd.AddCommand(newRunCmd(rc))
	return cmd
}

// notifyContext returns a context that is cancelled on SIGINT / SIGTERM.
func notifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

func newJournalCmd(rc *trader.RootConfig) *cobra.Command {
	var (
		accountID    string
		token        string
		env          string
		journalKind  string
		csvTrades    string
		csvEquity    string
		sqlitePath   string
		backfillFrom int64
	)
	cmd := &cobra.Command{
		Use:   "journal",
		Short: "Subscribe to OANDA transactions and journal closed trades",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := notifyContext(cmd.Context())
			defer cancel()

			svc, err := service.New(service.Config{Env: env, Token: token, AccountID: accountID})
			if err != nil {
				return err
			}
			if err := svc.ResolveAccount(ctx); err != nil {
				var amb service.AmbiguousAccountError
				if errors.As(err, &amb) {
					fmt.Println("Multiple accounts found — specify one with --account-id:")
					for _, id := range amb.Accounts {
						fmt.Printf("  %s\n", id)
					}
				}
				return err
			}

			journal, err := svc.OpenJournal(service.JournalConfig{
				Kind:       journalKind,
				CSVTrades:  csvTrades,
				CSVEquity:  csvEquity,
				SQLitePath: sqlitePath,
			})
			if err != nil {
				return err
			}
			defer journal.Close()

			if backfillFrom > 0 {
				fmt.Printf("Backfilling transactions from ID %d...\n", backfillFrom)
			}
			fmt.Printf("Live journal subscribed to %s (journal=%s). Ctrl-C to exit.\n", svc.AccountID, journalKind)

			lastID, err := svc.RunLiveJournal(ctx, journal, backfillFrom)
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
	cmd.Flags().StringVar(&journalKind, "journal", "csv", "Journal backend: csv|sqlite")
	cmd.Flags().StringVar(&csvTrades, "csv-trades", "live-trades.csv", "Path for CSV trades file (when --journal=csv)")
	cmd.Flags().StringVar(&csvEquity, "csv-equity", "live-equity.csv", "Path for CSV equity file (when --journal=csv)")
	cmd.Flags().StringVar(&sqlitePath, "sqlite", "live.db", "Path for SQLite db (when --journal=sqlite)")
	cmd.Flags().Int64Var(&backfillFrom, "backfill-from", 0, "If >0, poll GetTransactions from this ID before starting the stream")
	return cmd
}
