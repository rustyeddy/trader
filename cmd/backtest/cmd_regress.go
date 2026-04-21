package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

var regressConfigsDir string
var regressOutDir string

var CMDBacktestRegress = &cobra.Command{
	Use:   "regress",
	Short: "Run committed regression backtests and write fresh JSON summaries",
	RunE:  runBacktestRegress,
}

func init() {
	CMDBacktestRegress.Flags().StringVar(
		&regressConfigsDir,
		"configs",
		"../testdata/configs",
		"Regression config file or directory",
	)
	CMDBacktestRegress.Flags().StringVar(
		&regressOutDir,
		"out",
		"../testdata/reports",
		"Output directory for generated regression summaries (default: temporary directory)",
	)
}

func runBacktestRegress(cmd *cobra.Command, args []string) error {
	_ = cmd
	_ = args

	configPath := strings.TrimSpace(regressConfigsDir)
	if configPath == "" {
		configPath = "./testdata/configs"
	}

	configPaths, err := collectConfigPaths(configPath)
	if err != nil {
		return err
	}

	outDir := strings.TrimSpace(regressOutDir)
	createdTemp := false
	if outDir == "" {
		outDir, err = os.MkdirTemp("", "trader-regress-*")
		if err != nil {
			return fmt.Errorf("create temp output dir: %w", err)
		}
		createdTemp = true
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("create output dir %q: %w", outDir, err)
		}
	}

	count := 0
	for _, cfgPath := range configPaths {
		cfg, err := trader.LoadConfig(cfgPath)
		if err != nil {
			return fmt.Errorf("load config %q: %w", cfgPath, err)
		}

		runs, err := cfg.ResolveAllRuns()
		if err != nil {
			return fmt.Errorf("resolve runs from %q: %w", cfgPath, err)
		}

		// Phase 1 regression is intentionally simple:
		// one config file -> one run -> one summary json
		if len(runs) != 1 {
			return fmt.Errorf(
				"regression config %q must resolve to exactly 1 run, got %d",
				cfgPath,
				len(runs),
			)
		}

		run, err := executeConfiguredRun(context.Background(), runs[0])
		if err != nil {
			return fmt.Errorf("execute run from %q: %w", cfgPath, err)
		}

		summary := trader.NewBacktestReportSummary(run)
		reportPath := regressionReportPath(outDir, cfgPath)

		if err := writeRegressionSummary(reportPath, summary); err != nil {
			return fmt.Errorf("write regression summary for %q: %w", cfgPath, err)
		}

		fmt.Fprintf(os.Stdout, "Generated: %s\n", reportPath)
		count++
	}

	if count == 0 {
		return fmt.Errorf("no regression configs found in %q", configPath)
	}

	if createdTemp {
		fmt.Fprintf(os.Stdout, "\nTemporary output directory: %s\n", outDir)
	} else {
		fmt.Fprintf(os.Stdout, "\nOutput directory: %s\n", outDir)
	}

	return nil
}

func regressionReportPath(outDir, cfgPath string) string {
	base := strings.TrimSuffix(filepath.Base(cfgPath), filepath.Ext(cfgPath))
	return filepath.Join(outDir, base+".json")
}

func writeRegressionSummary(path string, summary trader.BacktestReportSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(path), err)
	}

	b, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	b = append(b, '\n')

	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}

	return nil
}
