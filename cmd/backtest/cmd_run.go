package backtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/service"
)

// backtestBaseDir returns the root directory for production backtest configs
// and reports. It honours the TRADER_BACKTEST_DIR environment variable; if
// unset it falls back to /srv/trading/backtests.
//
// Layout under this directory:
//
//	configs/   — YAML backtest configs
//	reports/   — hash-named JSON + org reports
func backtestBaseDir() string {
	if d := strings.TrimSpace(os.Getenv("TRADER_BACKTEST_DIR")); d != "" {
		return d
	}
	return "/srv/trading/backtests"
}

var (
	runConfigPath string
	runOutDir     string
)

// CMDBacktestRun runs one or more backtest configs and writes reports named
// <run-name>-<config-hash>.json to the output directory. Re-running the same
// config overwrites the same file; changing any param produces a new file.
var CMDBacktestRun = &cobra.Command{
	Use:   "run [config-path]",
	Short: "Run backtest configs and write JSON + org reports",
	Long: `Run backtest configs and write reports to the reports directory.

Report filenames are derived from a hash of the run parameters, so
re-running the same config overwrites the same file rather than
creating a new timestamped copy. Changing any parameter produces a
distinct file alongside the previous one.

Config and result directories default to $TRADER_BACKTEST_DIR/{configs,reports}
(falling back to /srv/trading/backtests/{configs,reports} when the env var is unset).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBacktestRun,
}

func init() {
	CMDBacktestRun.Flags().StringVar(
		&runConfigPath,
		"config",
		"",
		fmt.Sprintf("Backtest config file, directory, or glob (default: $TRADER_BACKTEST_DIR/configs or %s/configs)", backtestBaseDir()),
	)
	CMDBacktestRun.Flags().StringVar(
		&runOutDir,
		"out",
		"",
		fmt.Sprintf("Output directory for reports (default: $TRADER_BACKTEST_DIR/reports or %s/reports)", backtestBaseDir()),
	)
}

func runBacktestRun(cmd *cobra.Command, args []string) error {
	base := backtestBaseDir()

	configPath := backtestRunConfigPath(base, args, runConfigPath, rootCfg)

	outDir := strings.TrimSpace(runOutDir)
	if outDir == "" {
		outDir = filepath.Join(base, "reports")
	}

	svc := &service.Service{Log: l}
	summaries, err := svc.RunBacktestPathSpecsAndWriteReports(cmd.Context(), []string{configPath}, outDir)
	if err != nil {
		return err
	}

	for _, summary := range summaries {
		backtest.PrintSummary(os.Stdout, summary)
		l.Info("wrote reports", "name", summary.Name, "config_hash", summary.ConfigHash, "dir", outDir)
	}

	fmt.Fprintf(os.Stdout, "\nOutput directory: %s\n", outDir)
	return nil
}

func backtestRunConfigPath(base string, args []string, localConfig string, root *trader.RootConfig) string {
	if len(args) > 0 {
		return args[0]
	}
	if path := strings.TrimSpace(localConfig); path != "" {
		return path
	}
	if root != nil {
		if path := strings.TrimSpace(root.ConfigPath); path != "" {
			return path
		}
	}
	return filepath.Join(base, "configs")
}
