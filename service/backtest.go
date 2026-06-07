package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rustyeddy/trader"
)

// RunBacktest executes one compiled backtest definition end-to-end and returns
// the rendered summary. The Trader/Broker/Account are wired up fresh per call;
// service does not retain execution state between runs.
func (s *Service) RunBacktest(ctx context.Context, compiled trader.CompiledBacktest) (trader.BacktestReportSummary, error) {
	run := compiled.NewRun()
	if run.BacktestRequest == nil {
		return trader.BacktestReportSummary{}, fmt.Errorf("nil backtest request")
	}
	if err := s.backtestExecutor().Execute(ctx, &run); err != nil {
		return trader.BacktestReportSummary{}, fmt.Errorf("backtest %q: %w", run.Name, err)
	}
	return run.Summary(), nil
}

func (s *Service) backtestExecutor() trader.BacktestExecutor {
	if s != nil && s.Backtests != nil {
		return s.Backtests
	}
	return trader.NewTraderBacktestExecutor(trader.GetDataManager())
}

// RunBacktestConfigs loads a slice of YAML config files, expands each
// into per-run *Backtest objects, and executes them all. Returns the
// summaries in submission order along with any per-run errors (errors
// are non-fatal: one bad run doesn't abort the others).
//
// This is the typical "regression sweep" entry point used by both the
// CLI and the future REST endpoint.
func (s *Service) RunBacktestConfigs(ctx context.Context, configPaths []string) ([]trader.BacktestReportSummary, error) {
	var summaries []trader.BacktestReportSummary

	for _, cfgPath := range configPaths {
		cfg, err := trader.LoadConfig(cfgPath)
		if err != nil {
			return summaries, fmt.Errorf("load config %q: %w", cfgPath, err)
		}
		runs, err := trader.CompileBacktests(cfg)
		if err != nil {
			s.Log.Warn("service: skipping config", "path", cfgPath, "err", err)
			continue
		}
		for _, run := range runs {
			run := run
			summary, runErr := s.RunBacktest(ctx, run)
			if runErr != nil {
				s.Log.Warn("service: backtest run failed",
					"name", run.Request.Name, "err", runErr)
				continue
			}
			summaries = append(summaries, summary)
		}
	}
	return summaries, nil
}

// ResolveBacktestConfigPaths expands backtest path specs into concrete config
// files. Each spec may be a file, a directory, or a glob pattern. Directories
// expand to sorted *.yml, *.yaml, and *.json files.
func ResolveBacktestConfigPaths(pathSpecs []string) ([]string, error) {
	if len(pathSpecs) == 0 {
		return nil, fmt.Errorf("config_paths is required")
	}

	seen := make(map[string]struct{})
	var resolved []string

	appendUnique := func(paths []string) {
		for _, p := range paths {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			resolved = append(resolved, p)
		}
	}

	for _, spec := range pathSpecs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			return nil, fmt.Errorf("config path must not be blank")
		}

		if hasGlobMeta(spec) {
			matches, err := filepath.Glob(spec)
			if err != nil {
				return nil, fmt.Errorf("glob %q: %w", spec, err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("glob %q matched no config files", spec)
			}
			appendUnique(matches)
			continue
		}

		info, err := os.Stat(spec)
		if err != nil {
			return nil, fmt.Errorf("stat config path %q: %w", spec, err)
		}
		if !info.IsDir() {
			appendUnique([]string{spec})
			continue
		}

		matches, err := backtestConfigFilesInDir(spec)
		if err != nil {
			return nil, err
		}
		appendUnique(matches)
	}

	if len(resolved) == 0 {
		return nil, fmt.Errorf("no backtest config files resolved")
	}
	return resolved, nil
}

// RunBacktestPathSpecs resolves config path specs and executes them.
func (s *Service) RunBacktestPathSpecs(ctx context.Context, pathSpecs []string) ([]trader.BacktestReportSummary, error) {
	configPaths, err := ResolveBacktestConfigPaths(pathSpecs)
	if err != nil {
		return nil, err
	}
	return s.RunBacktestConfigs(ctx, configPaths)
}

// RunBacktestConfigsAndWriteReports executes the given configs and persists the
// resulting JSON + org reports into outDir using the repository's canonical
// hash-based naming scheme.
func (s *Service) RunBacktestConfigsAndWriteReports(ctx context.Context, configPaths []string, outDir string) ([]trader.BacktestReportSummary, error) {
	summaries, err := s.RunBacktestConfigs(ctx, configPaths)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, fmt.Errorf("no backtest results generated")
	}
	if err := WriteBacktestReports(outDir, summaries); err != nil {
		return nil, err
	}
	return summaries, nil
}

// RunBacktestPathSpecsAndWriteReports resolves config path specs, executes the
// resulting backtests, and persists canonical report artifacts in outDir.
func (s *Service) RunBacktestPathSpecsAndWriteReports(ctx context.Context, pathSpecs []string, outDir string) ([]trader.BacktestReportSummary, error) {
	configPaths, err := ResolveBacktestConfigPaths(pathSpecs)
	if err != nil {
		return nil, err
	}
	return s.RunBacktestConfigsAndWriteReports(ctx, configPaths, outDir)
}

// WriteBacktestReports persists each summary as JSON + org and rebuilds
// index.org in dir.
func WriteBacktestReports(dir string, summaries []trader.BacktestReportSummary) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir %q: %w", dir, err)
	}
	for _, summary := range summaries {
		stem := backtestReportStem(summary)
		if err := WriteBacktestSummaryJSON(filepath.Join(dir, stem+".json"), summary); err != nil {
			return fmt.Errorf("write json for %q: %w", summary.Name, err)
		}
		if err := WriteBacktestSummaryOrg(filepath.Join(dir, stem+".org"), summary); err != nil {
			return fmt.Errorf("write org for %q: %w", summary.Name, err)
		}
	}
	if err := RebuildBacktestIndex(dir); err != nil {
		return fmt.Errorf("write index.org: %w", err)
	}
	return nil
}

// WriteBacktestSummaryJSON marshals s as indented JSON to path, creating parent
// directories as needed.
func WriteBacktestSummaryJSON(path string, s trader.BacktestReportSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// WriteBacktestSummaryOrg writes a full org-mode report for one backtest run to
// path, creating parent directories as needed.
func WriteBacktestSummaryOrg(path string, s trader.BacktestReportSummary) error {
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

// RebuildBacktestIndex scans dir for persisted report JSON files and rewrites
// index.org as a comparison table.
func RebuildBacktestIndex(dir string) error {
	summaries, err := ListBacktestSummaries(dir)
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
	return nil
}

// ListBacktestSummaries reads persisted JSON summaries from dir and returns
// them in reverse lexical filename order.
func ListBacktestSummaries(dir string) ([]trader.BacktestReportSummary, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob summaries in %q: %w", dir, err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))

	var summaries []trader.BacktestReportSummary
	for _, path := range matches {
		if filepath.Base(path) == "index.json" {
			continue
		}
		s, err := ReadBacktestSummaryFile(path)
		if err != nil {
			continue
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
}

// ReadBacktestSummaryFile reads and parses a persisted backtest JSON summary
// from path.
func ReadBacktestSummaryFile(path string) (trader.BacktestReportSummary, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return trader.BacktestReportSummary{}, err
	}
	var s trader.BacktestReportSummary
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	base := filepath.Base(path)
	s.Name = strings.TrimSuffix(base, ".json")
	return s, nil
}

// ReadBacktestSummaryByName reads a persisted backtest JSON summary from dir by
// logical report name.
func ReadBacktestSummaryByName(dir, name string) (trader.BacktestReportSummary, error) {
	name = filepath.Base(name)
	if !strings.HasSuffix(name, ".json") {
		name += ".json"
	}
	return ReadBacktestSummaryFile(filepath.Join(dir, name))
}

// ListBacktestOrgReports returns the basenames of persisted org reports in dir
// in lexical order.
func ListBacktestOrgReports(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.org"))
	if err != nil {
		return nil, fmt.Errorf("glob org reports in %q: %w", dir, err)
	}
	sort.Strings(matches)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	return names, nil
}

// ReadBacktestOrgReport reads a persisted org report from dir by logical report
// name and returns the file content plus the canonical filename.
func ReadBacktestOrgReport(dir, name string) ([]byte, string, error) {
	name = filepath.Base(name)
	if !strings.HasSuffix(name, ".org") {
		name += ".org"
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, name, err
	}
	return data, name, nil
}

func backtestReportStem(summary trader.BacktestReportSummary) string {
	hash := strings.TrimSpace(summary.ConfigHash)
	if hash == "" {
		return summary.Name
	}
	return summary.Name + "-" + hash
}

func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func backtestConfigFilesInDir(dir string) ([]string, error) {
	var matches []string
	for _, pattern := range []string{"*.yml", "*.yaml", "*.json"} {
		m, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, fmt.Errorf("glob %q in %q: %w", pattern, dir, err)
		}
		matches = append(matches, m...)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("config directory %q contains no .yml/.yaml/.json files", dir)
	}
	sort.Strings(matches)
	return matches, nil
}
