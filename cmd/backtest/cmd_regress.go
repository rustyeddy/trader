package backtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

const defaultRegressionConfigPath = "testdata/configs"
const defaultOutDir = "../trading/backtests"

var regressOutDir string
var l = trader.L

var CMDBacktestRegress = &cobra.Command{
	Use:   "regress",
	Short: "Run config-based regression backtests and write JSON + org reports",
	RunE:  runBacktestRegress,
}

func init() {
	CMDBacktestRegress.Flags().StringVar(
		&regressOutDir,
		"out",
		"",
		fmt.Sprintf("Output directory for reports (default: %s)", defaultOutDir),
	)
}

func runBacktestRegress(cmd *cobra.Command, args []string) error {
	configPath := defaultRegressionConfigPath
	if rootCfg != nil && strings.TrimSpace(rootCfg.ConfigPath) != "" {
		configPath = strings.TrimSpace(rootCfg.ConfigPath)
	}

	configPaths, err := collectConfigPaths(configPath)
	if err != nil {
		return err
	}

	outDir := strings.TrimSpace(regressOutDir)
	if outDir == "" {
		outDir = defaultOutDir
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir %q: %w", outDir, err)
	}

	count := 0
	for _, cfgPath := range configPaths {
		cfg, err := trader.LoadConfig(cfgPath)
		if err != nil {
			return fmt.Errorf("load config %q: %w", cfgPath, err)
		}

		runs, err := trader.GetBacktests(cfg)
		if err != nil {
			fmt.Printf("skipping config %q: %v\n", cfgPath, err)
			continue
		}

		for _, run := range runs {
			ctx := cmd.Context()
			t := &trader.Trader{
				DataManager: trader.GetDataManager(),
			}
			t.Broker = trader.NewBroker("sim")
			acct := trader.NewAccount("backtest", run.StartingBalance)
			if run.RiskPct != 0 {
				acct.RiskPct = run.RiskPct
			}
			t.Broker.Account = acct

			if err := t.Backtest(ctx, &run); err != nil {
				fmt.Printf("backtest error: %v\n", err)
				continue
			}

			summary := run.Summary()
			trader.PrintSummary(os.Stdout, summary)

			if err := writeJSON(filepath.Join(outDir, run.Name+".json"), summary); err != nil {
				return fmt.Errorf("write json for %q: %w", run.Name, err)
			}
			if err := writeOrg(filepath.Join(outDir, run.Name+".org"), summary); err != nil {
				return fmt.Errorf("write org for %q: %w", run.Name, err)
			}

			l.Info("wrote reports", "name", run.Name, "dir", outDir)
			count++
		}
	}

	if count == 0 {
		return fmt.Errorf("no regression configs found in %q", configPath)
	}

	// Rebuild index.org from all JSON files in the output directory.
	if err := rebuildIndex(outDir); err != nil {
		l.Warn("could not write index.org", "err", err)
	}

	fmt.Fprintf(os.Stdout, "\nOutput directory: %s\n", outDir)
	return nil
}

// writeJSON marshals the summary as indented JSON.
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

// writeOrg writes a full org-mode report for a single backtest run.
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

// rebuildIndex scans dir for all *.json files, loads their summaries,
// and writes a fresh index.org comparison table.
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

// writeRegressionSummary is kept for any callers outside this file.
func writeRegressionSummary(path string, summary trader.BacktestReportSummary) error {
	return writeJSON(path, summary)
}
