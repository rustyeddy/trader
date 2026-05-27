package backtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
)

// backtestBaseDir returns the root directory for production backtest configs
// and results. It honours the TRADER_BACKTEST_DIR environment variable; if
// unset it falls back to /trading/backtests.
//
// Layout under this directory:
//
//	configs/   — YAML backtest configs
//	results/   — hash-named JSON + org reports
func backtestBaseDir() string {
	if d := strings.TrimSpace(os.Getenv("TRADER_BACKTEST_DIR")); d != "" {
		return d
	}
	return "/trading/backtests"
}

var runOutDir string

// CMDBacktestRun runs one or more backtest configs and writes reports named
// <run-name>-<config-hash>.json to the output directory. Re-running the same
// config overwrites the same file; changing any param produces a new file.
var CMDBacktestRun = &cobra.Command{
	Use:   "run [config-path]",
	Short: "Run backtest configs and write JSON + org reports",
	Long: `Run backtest configs and write reports to the results directory.

Report filenames are derived from a hash of the run parameters, so
re-running the same config overwrites the same file rather than
creating a new timestamped copy. Changing any parameter produces a
distinct file alongside the previous one.

Config and result directories default to $TRADER_BACKTEST_DIR/{configs,results}
(falling back to /trading/backtests/{configs,results} when the env var is unset).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBacktestRun,
}

func init() {
	CMDBacktestRun.Flags().StringVar(
		&runOutDir,
		"out",
		"",
		fmt.Sprintf("Output directory for reports (default: $TRADER_BACKTEST_DIR/results or %s/results)", backtestBaseDir()),
	)
}

func runBacktestRun(cmd *cobra.Command, args []string) error {
	base := backtestBaseDir()

	configPath := filepath.Join(base, "configs")
	if len(args) > 0 {
		configPath = args[0]
	} else if rootCfg != nil && strings.TrimSpace(rootCfg.ConfigPath) != "" {
		configPath = strings.TrimSpace(rootCfg.ConfigPath)
	}

	configPaths, err := collectConfigPaths(configPath)
	if err != nil {
		return err
	}

	outDir := strings.TrimSpace(runOutDir)
	if outDir == "" {
		outDir = filepath.Join(base, "results")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir %q: %w", outDir, err)
	}

	svc := &service.Service{Log: l}
	summaries, err := svc.RunBacktestConfigs(cmd.Context(), configPaths)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		return fmt.Errorf("no backtest configs found in %q", configPath)
	}

	for _, summary := range summaries {
		trader.PrintSummary(os.Stdout, summary)
		stem := summary.Name + "-" + summary.ConfigHash
		if err := writeJSON(filepath.Join(outDir, stem+".json"), summary); err != nil {
			return fmt.Errorf("write json for %q: %w", summary.Name, err)
		}
		if err := writeOrg(filepath.Join(outDir, stem+".org"), summary); err != nil {
			return fmt.Errorf("write org for %q: %w", summary.Name, err)
		}
		l.Info("wrote reports", "name", stem, "dir", outDir)
	}

	if err := rebuildIndex(outDir); err != nil {
		l.Warn("could not write index.org", "err", err)
	}

	fmt.Fprintf(os.Stdout, "\nOutput directory: %s\n", outDir)
	return nil
}

// writeJSON marshals s as indented JSON to path, creating parent directories
// as needed.
func writeJSON(path string, s trader.BacktestReportSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// writeOrg writes a full org-mode report for one backtest run to path.
func writeOrg(path string, s trader.BacktestReportSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	trader.WriteOrgReport(f, s)
	return nil
}

// rebuildIndex scans dir for *.json files (excluding index.json), loads their
// summaries, and rewrites index.org as a comparison table.
func rebuildIndex(dir string) error {
	summaries, err := trader.LoadOrgIndexSummaries(dir)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		return nil
	}
	path := filepath.Join(dir, "index.org")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	trader.WriteOrgIndex(f, summaries)
	l.Info("wrote index", "path", path)
	return nil
}
