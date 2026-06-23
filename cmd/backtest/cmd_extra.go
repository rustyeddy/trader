package backtest

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/service"
)

// ── backtest org ──────────────────────────────────────────────────────────

var orgReportsDir string

var CMDBacktestOrg = &cobra.Command{
	Use:   "org <name>",
	Short: "Print the org-mode report for a saved backtest result",
	Long: `Print the org-mode report for a named backtest result to stdout.

The name argument is the report filename without the .org extension.
Reports are read from $TRADER_BACKTEST_DIR/reports (or /srv/trading/backtests/reports).`,
	Args: cobra.ExactArgs(1),
	RunE: runBacktestOrg,
}

func init() {
	CMDBacktestOrg.Flags().StringVar(
		&orgReportsDir,
		"dir",
		"",
		fmt.Sprintf("Reports directory (default: $TRADER_BACKTEST_DIR/reports or %s/reports)", backtestBaseDir()),
	)
}

func runBacktestOrg(cmd *cobra.Command, args []string) error {
	name := args[0]
	dir := resolveReportsDir(orgReportsDir)

	data, _, err := service.ReadBacktestOrgReport(dir, name)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("org report %q not found in %s", name, dir)
		}
		return fmt.Errorf("read org report: %w", err)
	}

	_, err = cmd.OutOrStdout().Write(data)
	return err
}

// ── backtest candles ──────────────────────────────────────────────────────

var candlesReportsDir string

var CMDBacktestCandles = &cobra.Command{
	Use:   "candles <name>",
	Short: "Print OHLC candles for a saved backtest result as CSV",
	Long: `Print the OHLC candle data for the instrument/timeframe/range described
in a named backtest report. Output is CSV with columns: time,open,high,low,close.

The name argument is the report filename without the .json extension.`,
	Args: cobra.ExactArgs(1),
	RunE: runBacktestCandles,
}

func init() {
	CMDBacktestCandles.Flags().StringVar(
		&candlesReportsDir,
		"dir",
		"",
		fmt.Sprintf("Reports directory (default: $TRADER_BACKTEST_DIR/reports or %s/reports)", backtestBaseDir()),
	)
}

func runBacktestCandles(cmd *cobra.Command, args []string) error {
	name := args[0]
	dir := resolveReportsDir(candlesReportsDir)

	summary, err := service.ReadBacktestSummaryByName(dir, name)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("report %q not found in %s", name, dir)
		}
		return fmt.Errorf("read backtest result: %w", err)
	}

	cfg := summary.Config.Data
	tr, err := trader.ParseTimeRange(cfg.From, cfg.To, cfg.Timeframe)
	if err != nil {
		return fmt.Errorf("parse time range: %w", err)
	}

	dm := marketdata.NewDataManager([]string{cfg.Instrument}, tr.Start.Time(), tr.End.Time())
	iter, err := dm.Candles(cmd.Context(), marketdata.CandleRequest{
		Source:     cfg.Source,
		Instrument: cfg.Instrument,
		Range:      tr,
		Strict:     false,
	})
	if err != nil {
		return fmt.Errorf("load candles: %w", err)
	}
	defer func() { _ = iter.Close() }()

	w := csv.NewWriter(cmd.OutOrStdout())
	_ = w.Write([]string{"time", "open", "high", "low", "close"})
	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		c := ct.Candle
		_ = w.Write([]string{
			ct.Timestamp.Time().UTC().Format("2006-01-02T15:04:05Z"),
			fmt.Sprintf("%.5f", c.Open.Float64()),
			fmt.Sprintf("%.5f", c.High.Float64()),
			fmt.Sprintf("%.5f", c.Low.Float64()),
			fmt.Sprintf("%.5f", c.Close.Float64()),
		})
	}
	w.Flush()
	if err := iter.Err(); err != nil {
		return fmt.Errorf("read candles: %w", err)
	}
	return w.Error()
}

// ── backtest configs ──────────────────────────────────────────────────────

var configsDir string

var CMDBacktestConfigs = &cobra.Command{
	Use:   "configs",
	Short: "List available backtest config files",
	Long: `List YAML/JSON backtest config files in the configs directory.

Defaults to $TRADER_BACKTEST_DIR/configs (or /srv/trading/backtests/configs).
Use --dir to point at a different directory.`,
	RunE: runBacktestConfigs,
}

func init() {
	CMDBacktestConfigs.Flags().StringVar(
		&configsDir,
		"dir",
		"",
		fmt.Sprintf("Configs directory (default: $TRADER_BACKTEST_DIR/configs or %s/configs)", backtestBaseDir()),
	)
}

func runBacktestConfigs(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	dir := strings.TrimSpace(configsDir)
	if dir == "" {
		dir = filepath.Join(backtestBaseDir(), "configs")
	}

	var matches []string
	for _, pat := range []string{"*.yml", "*.yaml", "*.json"} {
		m, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			return fmt.Errorf("glob %s in %s: %w", pat, dir, err)
		}
		matches = append(matches, m...)
	}
	sort.Strings(matches)

	if len(matches) == 0 {
		fmt.Fprintf(out, "No config files found in %s\n", dir)
		return nil
	}

	bar := strings.Repeat("─", 60)
	fmt.Fprintln(out, bar)
	for _, m := range matches {
		fmt.Fprintf(out, "  %s\n", filepath.Base(m))
	}
	fmt.Fprintln(out, bar)
	fmt.Fprintf(out, "  %d config(s)  dir: %s\n", len(matches), dir)
	return nil
}
