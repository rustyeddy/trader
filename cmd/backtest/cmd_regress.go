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

const defaultRegressionConfigPath = "../testdata/configs"

var regressOutDir string
var l = trader.L

var CMDBacktestRegress = &cobra.Command{
	Use:   "regress",
	Short: "Run config-based regression backtests and write fresh JSON summaries",
	RunE:  runBacktestRegress,
}

func init() {
	CMDBacktestRegress.Flags().StringVar(
		&regressOutDir,
		"out",
		"",
		"Output directory for generated regression summaries (default: temporary directory)",
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
			t.Broker.Account = trader.NewAccount("backtest", run.StartingBalance)

			err := t.Backtest(ctx, &run)
			if err != nil {
				fmt.Printf("Backtest errored %+v\n", err) // turn into a log
				continue
			}

			summary := run.Summary()

			reportPath := filepath.Join(outDir, run.Name+".json")
			if err := writeRegressionSummary(reportPath, summary); err != nil {
				return fmt.Errorf("write regression summary for %q: %w", cfgPath, err)
			}

			l.Info("generated regression summary", "path", reportPath)
			count++
		}

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
