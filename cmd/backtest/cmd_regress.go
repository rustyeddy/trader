package backtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
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

	// Backtest doesn't need OANDA, so construct a Service directly without
	// going through service.New (which requires a token).
	svc := &service.Service{Log: l}
	summaries, err := svc.RunBacktestConfigs(cmd.Context(), configPaths)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		return fmt.Errorf("no regression configs found in %q", configPath)
	}

	ts := time.Now().Format("20060102-150405")
	for _, summary := range summaries {
		trader.PrintSummary(os.Stdout, summary)
		stem := summary.Name + "_" + ts
		if err := writeJSON(filepath.Join(outDir, stem+".json"), summary); err != nil {
			return fmt.Errorf("write json for %q: %w", summary.Name, err)
		}
		if err := writeOrg(filepath.Join(outDir, stem+".org"), summary); err != nil {
			return fmt.Errorf("write org for %q: %w", summary.Name, err)
		}
		l.Info("wrote reports", "name", stem, "dir", outDir)
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
