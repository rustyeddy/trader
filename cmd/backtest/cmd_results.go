package backtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/backtest"
	backtestsvc "github.com/rustyeddy/trader/service/backtest"
)

// ── backtest list ─────────────────────────────────────────────────────────

var (
	listReportsDir string
	listInstrument string
	listStrategy   string
)

var CMDBacktestList = &cobra.Command{
	Use:   "list",
	Short: "List saved backtest results",
	Long: `List saved backtest JSON reports from the reports directory.

Reports default to $TRADER_BACKTEST_DIR/reports (or /srv/trading/backtests/reports).
Use --instrument or --strategy to filter results.`,
	RunE: runBacktestList,
}

func init() {
	CMDBacktestList.Flags().StringVar(
		&listReportsDir,
		"dir",
		"",
		fmt.Sprintf("Reports directory (default: $TRADER_BACKTEST_DIR/reports or %s/reports)", backtestBaseDir()),
	)
	CMDBacktestList.Flags().StringVar(&listInstrument, "instrument", "", "Filter by instrument (case-insensitive substring)")
	CMDBacktestList.Flags().StringVar(&listStrategy, "strategy", "", "Filter by strategy name (case-insensitive substring)")
}

func runBacktestList(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	dir := resolveReportsDir(listReportsDir)

	summaries, err := backtestsvc.ListBacktestSummaries(dir)
	if err != nil {
		return fmt.Errorf("list backtest results: %w", err)
	}

	// Apply filters.
	inst := strings.ToUpper(strings.TrimSpace(listInstrument))
	strat := strings.ToLower(strings.TrimSpace(listStrategy))
	filtered := summaries[:0]
	for _, s := range summaries {
		if inst != "" && !strings.Contains(strings.ToUpper(s.Instrument), inst) {
			continue
		}
		if strat != "" && !strings.Contains(strings.ToLower(s.Strategy), strat) {
			continue
		}
		filtered = append(filtered, s)
	}

	if len(filtered) == 0 {
		fmt.Fprintln(out, "No backtest results found.")
		return nil
	}

	bar := strings.Repeat("─", 100)
	fmt.Fprintln(out, bar)
	fmt.Fprintf(out, "  %-34s %-10s %-8s %-6s %6s %6s %8s %9s\n",
		"Name", "Instrument", "TF", "Trades", "Wins%", "RR", "Return%", "Net P/L")
	fmt.Fprintln(out, bar)
	for _, s := range filtered {
		rrStr := "—"
		if s.RR > 0 {
			rrStr = fmt.Sprintf("%.1f", s.RR)
		}
		sign := "+"
		if s.NetPL < 0 {
			sign = ""
		}
		fmt.Fprintf(out, "  %-34s %-10s %-8s %6d %5.1f%% %6s %7.2f%% %s$%.2f\n",
			truncate(s.Name, 34),
			s.Instrument,
			strings.ToUpper(s.Timeframe),
			s.Trades,
			s.WinRate,
			rrStr,
			s.ReturnPct,
			sign, s.NetPL)
	}
	fmt.Fprintln(out, bar)
	fmt.Fprintf(out, "  %d result(s)  dir: %s\n", len(filtered), dir)
	return nil
}

// ── backtest get ──────────────────────────────────────────────────────────

var getReportsDir string

var CMDBacktestGet = &cobra.Command{
	Use:   "get <name>",
	Short: "Show full details for a saved backtest result",
	Long: `Print the full summary for a named backtest report.

The name argument is the report filename without the .json extension.
Reports are read from $TRADER_BACKTEST_DIR/reports (or /srv/trading/backtests/reports).`,
	Args: cobra.ExactArgs(1),
	RunE: runBacktestGet,
}

func init() {
	CMDBacktestGet.Flags().StringVar(
		&getReportsDir,
		"dir",
		"",
		fmt.Sprintf("Reports directory (default: $TRADER_BACKTEST_DIR/reports or %s/reports)", backtestBaseDir()),
	)
}

func runBacktestGet(cmd *cobra.Command, args []string) error {
	name := args[0]
	dir := resolveReportsDir(getReportsDir)

	summary, err := backtestsvc.ReadBacktestSummaryByName(dir, name)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("report %q not found in %s", name, dir)
		}
		return fmt.Errorf("read backtest result: %w", err)
	}

	backtest.PrintSummary(cmd.OutOrStdout(), summary)
	return nil
}

// resolveReportsDir returns the effective reports directory, honouring an
// explicit override flag, then the TRADER_BACKTEST_DIR env var, then the
// default /srv/trading/backtests/reports path.
func resolveReportsDir(override string) string {
	if d := strings.TrimSpace(override); d != "" {
		return d
	}
	return filepath.Join(backtestBaseDir(), "reports")
}

// truncate shortens s to at most n runes, appending "…" if it was clipped.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
