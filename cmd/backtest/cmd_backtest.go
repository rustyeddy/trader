package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bt "github.com/rustyeddy/trader/backtest"
	cmdconfig "github.com/rustyeddy/trader/cmd/config"
	"github.com/spf13/cobra"
)

var rootCfg *cmdconfig.RootConfig

var (
	btRunName   string
	btStdout    bool
	btOutputDir string
)

func init() {
	CMDBacktest.AddCommand(CMDBacktestEMACross)
	CMDBacktest.Flags().StringVar(&btRunName, "run", "", "Run only the named config entry")
	CMDBacktest.Flags().BoolVar(&btStdout, "stdout", false, "Write reports to stdout instead of files")
	CMDBacktest.Flags().StringVar(&btOutputDir, "out", "", "Output directory for batch backtest reports")
}

func New(rc *cmdconfig.RootConfig) *cobra.Command {
	rootCfg = rc
	return CMDBacktest
}

var CMDBacktest = &cobra.Command{
	Use:   "backtest",
	Short: "Run backtests on historical data",
	RunE: func(cmd *cobra.Command, args []string) error {
		if rootCfg == nil || strings.TrimSpace(rootCfg.ConfigPath) == "" {
			return fmt.Errorf("backtest requires --config when run without a strategy subcommand")
		}
		return runConfiguredBatch(cmd, "")
	},
}

type batchIndex struct {
	Created   string           `json:"created"`
	Config    string           `json:"config,omitempty"`
	OutputDir string           `json:"output_dir,omitempty"`
	Summary   batchSummary     `json:"summary"`
	Runs      []batchRunRecord `json:"runs"`
}

type batchSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type batchRunRecord struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	TextPath string `json:"text_path,omitempty"`
	JSONPath string `json:"json_path,omitempty"`
}

func runConfiguredBatch(cmd *cobra.Command, kindFilter string) error {
	path := strings.TrimSpace(rootCfg.ConfigPath)
	cfg, err := bt.LoadConfig(path)
	if err != nil {
		return err
	}

	runs, err := selectRuns(cfg, btRunName)
	if err != nil {
		return err
	}

	index := batchIndex{
		Created: time.Now().UTC().Format(time.RFC3339),
		Config:  path,
	}

	outputDir := ""
	if !btStdout {
		outputDir, err = ensureBatchOutputDir(btOutputDir)
		if err != nil {
			return err
		}
		index.OutputDir = outputDir
	}

	for _, rr := range runs {
		if kindFilter != "" && !strings.EqualFold(strings.TrimSpace(rr.Strategy.Kind), kindFilter) {
			continue
		}

		rec := batchRunRecord{
			Name: rr.Name,
			Kind: strings.TrimSpace(rr.Strategy.Kind),
		}

		res, err := executeResolvedRun(context.Background(), cmd, rr)
		if err != nil {
			rec.Status = "failed"
			rec.Error = err.Error()
			index.Summary.Failed++
			index.Runs = append(index.Runs, rec)
			continue
		}

		rec.Status = "ok"
		index.Summary.Succeeded++

		if btStdout {
			bt.PrintBacktestRun(os.Stdout, res)
		} else {
			textPath, jsonPath, err := writeRunOutputs(outputDir, res)
			if err != nil {
				rec.Status = "failed"
				rec.Error = err.Error()
				index.Summary.Succeeded--
				index.Summary.Failed++
			} else {
				rec.TextPath = textPath
				rec.JSONPath = jsonPath
			}
		}

		index.Runs = append(index.Runs, rec)
	}

	index.Summary.Total = len(index.Runs)
	if kindFilter != "" && index.Summary.Total == 0 {
		return fmt.Errorf("config has no runs with strategy.kind=%q", kindFilter)
	}

	if !btStdout {
		if err := writeBatchIndex(outputDir, index); err != nil {
			return err
		}
	}

	printBatchSummary(os.Stdout, index)

	if index.Summary.Failed > 0 {
		return fmt.Errorf("%d backtest run(s) failed", index.Summary.Failed)
	}
	return nil
}

func selectRuns(cfg *bt.Config, requested string) ([]bt.ResolvedRun, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		rr, err := cfg.ResolveRun(requested)
		if err != nil {
			return nil, err
		}
		return []bt.ResolvedRun{*rr}, nil
	}
	return cfg.ResolveAllRuns()
}

func ensureBatchOutputDir(requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		requested = filepath.Join("backtests", time.Now().UTC().Format("20060102-150405"))
	}
	if err := os.MkdirAll(requested, 0o755); err != nil {
		return "", fmt.Errorf("create output dir %q: %w", requested, err)
	}
	return requested, nil
}

func writeRunOutputs(outputDir string, run bt.BacktestRun) (string, string, error) {
	base := sanitizeFilename(run.Name)
	if base == "" {
		base = "run"
	}
	kind := sanitizeFilename(run.Kind)
	if kind == "" {
		kind = "unknown"
	}
	prefix := fmt.Sprintf("%s.%s", base, kind)

	textPath := filepath.Join(outputDir, prefix+".txt")
	jsonPath := filepath.Join(outputDir, prefix+".json")

	textFile, err := os.Create(textPath)
	if err != nil {
		return "", "", fmt.Errorf("create text report %q: %w", textPath, err)
	}
	defer textFile.Close()
	bt.PrintBacktestRun(textFile, run)

	payload, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal json report for %q: %w", run.Name, err)
	}
	if err := os.WriteFile(jsonPath, payload, 0o644); err != nil {
		return "", "", fmt.Errorf("write json report %q: %w", jsonPath, err)
	}
	return textPath, jsonPath, nil
}

func writeBatchIndex(outputDir string, index batchIndex) error {
	payload, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal batch index: %w", err)
	}
	path := filepath.Join(outputDir, "index.json")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write batch index %q: %w", path, err)
	}
	return nil
}

func printBatchSummary(w *os.File, index batchIndex) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Backtest batch summary")
	fmt.Fprintln(w, "--------------------------------------------------")
	fmt.Fprintf(w, "Total:      %d\n", index.Summary.Total)
	fmt.Fprintf(w, "Succeeded:  %d\n", index.Summary.Succeeded)
	fmt.Fprintf(w, "Failed:     %d\n", index.Summary.Failed)
	if index.OutputDir != "" {
		fmt.Fprintf(w, "Output Dir: %s\n", index.OutputDir)
	}
	if len(index.Runs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Runs")
		fmt.Fprintln(w, "--------------------------------------------------")
		rows := append([]batchRunRecord(nil), index.Runs...)
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		for _, rec := range rows {
			line := fmt.Sprintf("- %s [%s]: %s", rec.Name, rec.Kind, rec.Status)
			if rec.Error != "" {
				line += " - " + rec.Error
			}
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintln(w)
}

func sanitizeFilename(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
