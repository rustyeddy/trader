package signalreplay

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/log"
	backtestsvc "github.com/rustyeddy/trader/service/backtest"
)

func cmdRun() *cobra.Command {
	var f genFlags
	var reportDir string
	cmd := &cobra.Command{
		Use:   "run [config-path]",
		Short: "Generate (if needed) and execute a signalreplay backtest config",
		Long: `run compiles and executes a signalreplay backtest config through the same
path as "trader backtest run".

Given an existing config path, it executes that config directly. Otherwise
it generates one from --signals/--exit (same flags as "gen"; --out is
optional and defaults to a temp file) and executes the result.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := resolveRunConfigPath(args, &f)
			if err != nil {
				return err
			}

			if reportDir == "" {
				return fmt.Errorf("signalreplay run: --report-dir is required")
			}

			svc := &backtestsvc.Service{Log: log.L}
			summaries, err := svc.RunBacktestPathSpecsAndWriteReports(cmd.Context(), []string{configPath}, reportDir)
			if err != nil {
				return err
			}
			for _, summary := range summaries {
				backtest.PrintSummary(cmd.OutOrStdout(), summary)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nReport directory: %s\n", reportDir)
			return nil
		},
	}
	addGenFlags(cmd, &f)
	cmd.Flags().StringVar(&f.out, "out", "", "Path to write the generated config (default: a temp file), ignored if a config-path argument is given")
	cmd.Flags().StringVar(&reportDir, "report-dir", "", "Output directory for backtest reports (required)")
	return cmd
}

// resolveRunConfigPath returns the config to execute: the positional
// argument if given, otherwise a freshly generated config written to
// f.out (or a temp file if --out was not set).
func resolveRunConfigPath(args []string, f *genFlags) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}

	if err := f.resolve(); err != nil {
		return "", err
	}
	cfg, err := GenerateConfig(f.opts)
	if err != nil {
		return "", err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("signalreplay run: marshal generated config: %w", err)
	}

	out := f.out
	if out == "" {
		tmp, err := os.CreateTemp("", "signalreplay-*.yml")
		if err != nil {
			return "", fmt.Errorf("signalreplay run: create temp config: %w", err)
		}
		out = tmp.Name()
		tmp.Close()
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		return "", fmt.Errorf("signalreplay run: write %q: %w", out, err)
	}
	return out, nil
}
