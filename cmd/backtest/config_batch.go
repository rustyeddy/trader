package backtest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

func runBacktestConfigBatch(cmd *cobra.Command) error {
	_ = cmd

	configPath := strings.TrimSpace(rootCfg.ConfigPath)
	if configPath == "" {
		return fmt.Errorf("missing --config path")
	}

	configPaths, err := collectConfigPaths(configPath)
	if err != nil {
		return err
	}

	reportsDir := strings.TrimSpace(btReportsDir)
	if reportsDir == "" {
		reportsDir = defaultReportsDir
	}

	totalRuns := 0
	for _, cfgPath := range configPaths {
		fmt.Fprintf(os.Stdout, "== Backtest config: %s ==\n", cfgPath)

		cfg, err := trader.LoadConfig(cfgPath)
		if err != nil {
			return err
		}

		runs, err := cfg.ResolveAllRuns()
		if err != nil {
			return fmt.Errorf("resolve runs from %q: %w", cfgPath, err)
		}

		for _, rr := range runs {
			totalRuns++

			run, err := executeConfiguredRun(context.Background(), rr)
			if err != nil {
				return fmt.Errorf("execute run %q from %q: %w", rr.Name, cfgPath, err)
			}

			trader.PrintBacktestRun(os.Stdout, run)

			reportPath, err := writeTextReport(reportsDir, cfgPath, run)
			if err != nil {
				return fmt.Errorf("write report for run %q: %w", rr.Name, err)
			}
			fmt.Fprintf(os.Stdout, "Report written: %s\n\n", reportPath)
		}
	}

	if totalRuns == 0 {
		return fmt.Errorf("no backtest runs found in %q", configPath)
	}

	return nil
}

func executeConfiguredRun(ctx context.Context, rr trader.ResolvedRun) (trader.BacktestRun, error) {
	opts := newCandleCmdCommon()
	applyCommonOptsFromResolvedRun(&opts, &rr)
	if opts.Units == 0 {
		return trader.BacktestRun{}, fmt.Errorf("run %q resolved units to 0; set defaults.units or strategy.params.units until risk-based sizing is implemented", rr.Name)
	}

	opts.Instrument = trader.NormalizeInstrument(opts.Instrument)

	meta := candleRunMeta{
		RunID:   trader.NewULID(),
		RunName: rr.Name,
		Kind:    rr.Strategy.Kind,
		Created: trader.FromTime(time.Now().UTC()),
		Balance: rr.StartingBalance,
		RR:      rr.RR,
	}

	acct := trader.NewAccount(rr.Name, rr.StartingBalance)
	kind := strings.ToLower(strings.TrimSpace(rr.Strategy.Kind))

	switch kind {
	case "ema-cross":
		cfg, err := BuildEMACrossConfig(rr)
		if err != nil {
			return trader.BacktestRun{}, err
		}
		strat := trader.NewEMACross(cfg)
		meta.Strategy = strat.Name()
		return executeCandleStrategy(ctx, opts, strat, meta, acct)
	case "ema-cross-adx":
		cfg, err := buildEMACrossADXConfig(rr)
		if err != nil {
			return trader.BacktestRun{}, err
		}
		strat := trader.NewEMACrossADX(cfg)
		meta.Strategy = strat.Name()
		return executeCandleStrategy(ctx, opts, strat, meta, acct)
	case "fake":
		strat := newConfigFakeStrategy(rr.Instrument)
		meta.Strategy = strat.Name()
		return executeCandleStrategy(ctx, opts, strat, meta, acct)

	case "fake-02":
		strat := newConfigFakeStrategy(rr.Instrument)
		meta.Strategy = strat.Name()
		return executeCandleStrategy(ctx, opts, strat, meta, acct)

	case "template":
		cfg, err := BuildTemplateStrategyConfig(rr)
		if err != nil {
			return trader.BacktestRun{}, err
		}
		strat := trader.NewTemplateStrategy(cfg)
		meta.Strategy = strat.Name()
		return executeCandleStrategy(ctx, opts, strat, meta, acct)
	default:
		return trader.BacktestRun{}, fmt.Errorf("unsupported strategy.kind %q", rr.Strategy.Kind)
	}
}

func collectConfigPaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat config path %q: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	matches, err := filepath.Glob(filepath.Join(path, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("glob config path %q: %w", path, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("config directory %q contains no *.yml files", path)
	}

	sort.Strings(matches)
	return matches, nil
}

func writeTextReport(reportsDir, configPath string, run trader.BacktestRun) (string, error) {
	reportPath := reportOutputPath(reportsDir, configPath, run)
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir reports dir: %w", err)
	}

	buf := new(bytes.Buffer)
	trader.PrintBacktestRun(buf, run)
	if err := os.WriteFile(reportPath, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("write report %q: %w", reportPath, err)
	}
	return reportPath, nil
}

func reportOutputPath(reportsDir, configPath string, run trader.BacktestRun) string {
	configBase := slug(strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath)))
	if configBase == "" {
		configBase = "config"
	}

	fileBase := slug(run.Name)
	if fileBase == "" {
		fileBase = "run"
	}
	kind := slug(run.Kind)
	if kind != "" {
		fileBase += "--" + kind
	}

	return filepath.Join(reportsDir, configBase, fileBase+".txt")
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}

	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}
