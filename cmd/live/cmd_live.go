// Package live hosts CLI commands for the live trading subsystem.
// Right now: 'trader live journal' subscribes to OANDA transactions and
// writes a TradeRecord per closed trade to CSV or SQLite.
package live

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/brokers/oanda"
)

func New(rc *trader.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "live",
		Short: "Live trading subsystem",
	}
	cmd.AddCommand(newJournalCmd(rc))
	return cmd
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
			if token == "" {
				token = readTokenFile()
			}
			if token == "" {
				return fmt.Errorf("no OANDA token: set OANDA_TOKEN, use --token, or save to ~/.config/oanda/pat.txt")
			}

			baseURL, err := oanda.BaseURL(env)
			if err != nil {
				return err
			}
			client := &oanda.Client{BaseURL: baseURL, Token: token}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			if accountID == "" {
				accounts, err := client.GetAccounts(ctx)
				if err != nil {
					return fmt.Errorf("discover accounts: %w", err)
				}
				if len(accounts) == 0 {
					return fmt.Errorf("no accounts found for this token")
				}
				if len(accounts) > 1 {
					fmt.Println("Multiple accounts found — specify one with --account-id:")
					for _, a := range accounts {
						fmt.Printf("  %s\n", a.ID)
					}
					return fmt.Errorf("ambiguous account")
				}
				accountID = accounts[0].ID
			}

			// Open the configured journal.
			var journal trader.Journal
			switch strings.ToLower(strings.TrimSpace(journalKind)) {
			case "csv":
				j, err := trader.NewCSV(csvTrades, csvEquity)
				if err != nil {
					return fmt.Errorf("open csv journal: %w", err)
				}
				journal = j
			case "sqlite":
				j, err := trader.NewSQLite(sqlitePath)
				if err != nil {
					return fmt.Errorf("open sqlite journal: %w", err)
				}
				journal = j
			default:
				return fmt.Errorf("--journal must be 'csv' or 'sqlite', got %q", journalKind)
			}
			defer journal.Close()

			lj := trader.NewLiveJournal(client, accountID, journal, slog.Default())

			if backfillFrom > 0 {
				fmt.Printf("Backfilling transactions from ID %d...\n", backfillFrom)
				if err := lj.Backfill(ctx, backfillFrom); err != nil {
					return fmt.Errorf("backfill: %w", err)
				}
			}

			fmt.Printf("Live journal subscribed to %s (journal=%s). Ctrl-C to exit.\n", accountID, journalKind)
			if err := lj.Run(ctx); err != nil && ctx.Err() == nil {
				return fmt.Errorf("live journal stream: %w", err)
			}
			fmt.Printf("Stopped. lastSeenTxID=%d\n", lj.LastSeenTxID())
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

func readTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "oanda", "pat.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
