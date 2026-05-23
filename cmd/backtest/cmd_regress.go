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

const defaultRegressConfigDir = "testdata/backtests/configs"
const defaultBaselineDir = "testdata/backtests/reports"

var regressUpdate bool
var regressBaselineDir string

// CMDBacktestRegress runs backtest configs and compares the results against
// committed JSON baselines. It exits non-zero when any metric differs.
// Pass --update to replace the baselines with the current results instead.
var CMDBacktestRegress = &cobra.Command{
	Use:   "regress",
	Short: "Compare backtest results against committed baselines; exit 1 on regression",
	RunE:  runBacktestRegress,
}

func init() {
	CMDBacktestRegress.Flags().BoolVar(
		&regressUpdate,
		"update",
		false,
		"Write current results as new baselines instead of comparing",
	)
	CMDBacktestRegress.Flags().StringVar(
		&regressBaselineDir,
		"baselines",
		defaultBaselineDir,
		"Directory containing committed baseline JSON reports",
	)
}

// regressResult holds the pass/fail outcome and diff lines for one run.
type regressResult struct {
	name   string
	passed bool
	diffs  []string
}

func runBacktestRegress(cmd *cobra.Command, args []string) error {
	configPath := defaultRegressConfigDir
	if rootCfg != nil && strings.TrimSpace(rootCfg.ConfigPath) != "" {
		configPath = strings.TrimSpace(rootCfg.ConfigPath)
	}

	configPaths, err := collectConfigPaths(configPath)
	if err != nil {
		return err
	}

	svc := &service.Service{Log: l}
	summaries, err := svc.RunBacktestConfigs(cmd.Context(), configPaths)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		return fmt.Errorf("no regression configs found in %q", configPath)
	}

	baselineDir := strings.TrimSpace(regressBaselineDir)

	if regressUpdate {
		return updateBaselines(baselineDir, summaries)
	}
	return compareBaselines(baselineDir, summaries)
}

// updateBaselines writes each summary as a baseline JSON file named
// <name>.json in dir, creating dir if needed.
func updateBaselines(dir string, summaries []trader.BacktestReportSummary) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create baseline dir %q: %w", dir, err)
	}
	for _, s := range summaries {
		path := baselinePath(dir, s.Name)
		if err := writeJSON(path, s); err != nil {
			return fmt.Errorf("write baseline for %q: %w", s.Name, err)
		}
		fmt.Printf("  updated  %s\n", path)
	}
	return nil
}

// compareBaselines loads the baseline for each summary and diffs every
// numeric metric. Prints a PASS/FAIL table to stdout and returns an error
// if any run regresses.
func compareBaselines(dir string, summaries []trader.BacktestReportSummary) error {
	var results []regressResult
	anyFailed := false

	for _, got := range summaries {
		path := baselinePath(dir, got.Name)
		baseline, err := loadBaseline(path)
		if err != nil {
			results = append(results, regressResult{
				name:   got.Name,
				passed: false,
				diffs:  []string{fmt.Sprintf("no baseline at %s — run with --update to create one", path)},
			})
			anyFailed = true
			continue
		}

		diffs := diffSummaries(*baseline, got)
		passed := len(diffs) == 0
		if !passed {
			anyFailed = true
		}
		results = append(results, regressResult{name: got.Name, passed: passed, diffs: diffs})
	}

	printRegressTable(results)

	if anyFailed {
		return fmt.Errorf("regression detected")
	}
	return nil
}

// printRegressTable writes a PASS/FAIL summary and any diff lines to stdout.
func printRegressTable(results []regressResult) {
	for _, r := range results {
		status := "PASS"
		if !r.passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %s\n", status, r.name)
		for _, d := range r.diffs {
			fmt.Printf("         %s\n", d)
		}
	}
}

// baselinePath returns the expected path for the baseline file for a run.
func baselinePath(dir, name string) string {
	return filepath.Join(dir, name+".json")
}

// loadBaseline reads and JSON-decodes a baseline file.
func loadBaseline(path string) (*trader.BacktestReportSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s trader.BacktestReportSummary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse baseline %q: %w", path, err)
	}
	return &s, nil
}

// diffSummaries returns a slice of human-readable difference strings between
// baseline and got. All comparisons are exact: since all values are derived
// from scaled-integer arithmetic, any change indicates a real regression.
func diffSummaries(baseline, got trader.BacktestReportSummary) []string {
	var diffs []string

	diffInt := func(field string, b, g int) {
		if b != g {
			diffs = append(diffs, fmt.Sprintf("%s: baseline=%d got=%d", field, b, g))
		}
	}
	diffFloat := func(field string, b, g float64) {
		if b != g {
			diffs = append(diffs, fmt.Sprintf("%s: baseline=%v got=%v", field, b, g))
		}
	}

	diffInt("trades", baseline.Trades, got.Trades)
	diffInt("wins", baseline.Wins, got.Wins)
	diffInt("losses", baseline.Losses, got.Losses)
	diffInt("spread_filtered", baseline.SpreadFiltered, got.SpreadFiltered)
	diffFloat("start_balance", baseline.StartBalance, got.StartBalance)
	diffFloat("end_balance", baseline.EndBalance, got.EndBalance)
	diffFloat("net_pl", baseline.NetPL, got.NetPL)
	diffFloat("return_pct", baseline.ReturnPct, got.ReturnPct)
	diffFloat("win_rate", baseline.WinRate, got.WinRate)
	diffFloat("max_drawdown", baseline.MaxDrawdown, got.MaxDrawdown)
	diffFloat("avg_winner", baseline.AvgWinner, got.AvgWinner)
	diffFloat("avg_loser", baseline.AvgLoser, got.AvgLoser)
	diffFloat("rr", baseline.RR, got.RR)
	diffFloat("avg_spread_pips", baseline.AvgSpreadPips, got.AvgSpreadPips)

	return diffs
}
