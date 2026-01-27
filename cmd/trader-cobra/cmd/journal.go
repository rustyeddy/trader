package cmd

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/journal"
	"github.com/spf13/cobra"
)

var journalCmd = &cobra.Command{
	Use:   "journal",
	Short: "Query trade journal data",
	Long: `Query and display trade journal records from SQLite database.

Subcommands:
  trade  - Get details of a specific trade by ID
  today  - List trades closed today
  day    - List trades closed on a specific day

Examples:
  trader journal trade <trade-id>
  trader journal today
  trader journal day 2024-01-15`,
}

var journalTradeCmd = &cobra.Command{
	Use:   "trade <trade-id>",
	Short: "Get details of a specific trade",
	Args:  cobra.ExactArgs(1),
	RunE:  runJournalTrade,
}

var journalTodayCmd = &cobra.Command{
	Use:   "today",
	Short: "List trades closed today",
	Args:  cobra.NoArgs,
	RunE:  runJournalToday,
}

var journalDayCmd = &cobra.Command{
	Use:   "day <YYYY-MM-DD>",
	Short: "List trades closed on a specific day",
	Args:  cobra.ExactArgs(1),
	RunE:  runJournalDay,
}

var journalDBPath string

func init() {
	rootCmd.AddCommand(journalCmd)
	journalCmd.AddCommand(journalTradeCmd)
	journalCmd.AddCommand(journalTodayCmd)
	journalCmd.AddCommand(journalDayCmd)

	journalCmd.PersistentFlags().StringVarP(&journalDBPath, "db", "d", "./trader.sqlite", "path to SQLite journal DB")
}

func runJournalTrade(cmd *cobra.Command, args []string) error {
	j, err := journal.NewSQLite(journalDBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer j.Close()

	tradeID := args[0]
	rec, err := j.GetTrade(tradeID)
	if err != nil {
		return fmt.Errorf("get trade: %w", err)
	}

	fmt.Println(journal.FormatTradeOrg(rec))
	return nil
}

func runJournalToday(cmd *cobra.Command, args []string) error {
	j, err := journal.NewSQLite(journalDBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer j.Close()

	loc := time.Local
	start, end, err := dayBounds(loc, time.Now().In(loc).Format("2006-01-02"))
	if err != nil {
		return fmt.Errorf("date: %w", err)
	}

	recs, err := j.ListTradesClosedBetween(start, end)
	if err != nil {
		return fmt.Errorf("query trades: %w", err)
	}

	fmt.Println(journal.FormatTradesOrg(recs))
	return nil
}

func runJournalDay(cmd *cobra.Command, args []string) error {
	j, err := journal.NewSQLite(journalDBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer j.Close()

	loc := time.Local
	day := args[0]
	start, end, err := dayBounds(loc, day)
	if err != nil {
		return fmt.Errorf("date: %w", err)
	}

	recs, err := j.ListTradesClosedBetween(start, end)
	if err != nil {
		return fmt.Errorf("query trades: %w", err)
	}

	fmt.Println(journal.FormatTradesOrg(recs))
	return nil
}

func dayBounds(loc *time.Location, day string) (time.Time, time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	end := start.Add(24 * time.Hour)
	return start, end, nil
}
