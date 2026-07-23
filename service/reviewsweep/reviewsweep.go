// Package reviewsweepsvc is the service-layer orchestration for running and
// persisting review sweeps, mirroring service/backtest's config/report
// mechanism (see that package's doc comment) but replaying
// review.ReviewPair's classification instead of executing a strategy.
//
// Named reviewsweepsvc, not reviewsweep, because the top-level
// "github.com/rustyeddy/trader/reviewsweep" package (config/report data
// model) is imported alongside this one in every real call site.
package reviewsweepsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rustyeddy/trader/review"
	"github.com/rustyeddy/trader/reviewsweep"
)

// dateLayout is the accepted format for RunConfig.From/To: a plain
// calendar date, parsed as UTC midnight — matches the `trader review`
// CLI's --from/--to flags.
const dateLayout = "2006-01-02"

// Service holds the small, self-contained dependency set review-sweep
// orchestration needs. No OANDA client: a review sweep only ever replays
// classification against the local candle store (review.RunSweep), by
// design — see review/replay.go.
type Service struct {
	Log *slog.Logger
}

// RunConfig executes one review-sweep run definition end-to-end and returns
// the rendered report summary.
func (s *Service) RunConfig(ctx context.Context, cfg reviewsweep.RunConfig) (reviewsweep.ReportSummary, error) {
	from, err := time.Parse(dateLayout, strings.TrimSpace(cfg.From))
	if err != nil {
		return reviewsweep.ReportSummary{}, fmt.Errorf("review sweep %q: parse from: %w", cfg.Name, err)
	}
	to, err := time.Parse(dateLayout, strings.TrimSpace(cfg.To))
	if err != nil {
		return reviewsweep.ReportSummary{}, fmt.Errorf("review sweep %q: parse to: %w", cfg.Name, err)
	}

	interval := 24 * time.Hour
	if v := strings.TrimSpace(cfg.Interval); v != "" {
		interval, err = time.ParseDuration(v)
		if err != nil {
			return reviewsweep.ReportSummary{}, fmt.Errorf("review sweep %q: parse interval: %w", cfg.Name, err)
		}
	}

	resp, err := review.RunSweep(ctx, s.Log, review.SweepRequest{
		Instruments: cfg.Instruments,
		From:        from,
		To:          to,
		Interval:    interval,
		Thresholds:  cfg.Thresholds,
	})
	if err != nil {
		return reviewsweep.ReportSummary{}, fmt.Errorf("review sweep %q: %w", cfg.Name, err)
	}

	return reviewsweep.ReportSummary{
		Name:        cfg.Name,
		ConfigHash:  reviewsweep.HashRunConfig(cfg),
		Instruments: cfg.Instruments,
		From:        cfg.From,
		To:          cfg.To,
		Interval:    ifEmpty(cfg.Interval, "24h"),
		GeneratedAt: time.Now().UTC(),
		Results:     resp.Results,
	}, nil
}

// RunConfigs loads a slice of YAML config files, expands each into
// per-run reviewsweep.RunConfig entries, and executes them all. Returns
// the summaries in submission order; one bad run doesn't abort the others.
func (s *Service) RunConfigs(ctx context.Context, configPaths []string) ([]reviewsweep.ReportSummary, error) {
	var summaries []reviewsweep.ReportSummary
	var errs []error

	for _, cfgPath := range configPaths {
		cfg, err := reviewsweep.LoadConfig(cfgPath)
		if err != nil {
			return summaries, fmt.Errorf("load config %q: %w", cfgPath, err)
		}
		for _, run := range cfg.Runs {
			summary, runErr := s.RunConfig(ctx, run)
			if runErr != nil {
				s.Log.Warn("service: review sweep run failed", "name", run.Name, "err", runErr)
				errs = append(errs, runErr)
				continue
			}
			summaries = append(summaries, summary)
		}
	}
	if len(summaries) == 0 && len(errs) > 0 {
		return summaries, errors.Join(errs...)
	}
	return summaries, nil
}

// ResolveConfigPaths expands review-sweep path specs into concrete config
// files. Each spec may be a file, a directory, or a glob pattern.
// Directories expand to sorted *.yml, *.yaml, and *.json files.
func ResolveConfigPaths(pathSpecs []string) ([]string, error) {
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

		if strings.ContainsAny(spec, "*?[") {
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

		matches, err := configFilesInDir(spec)
		if err != nil {
			return nil, err
		}
		appendUnique(matches)
	}

	if len(resolved) == 0 {
		return nil, fmt.Errorf("no review-sweep config files resolved")
	}
	return resolved, nil
}

// RunPathSpecs resolves config path specs and executes them.
func (s *Service) RunPathSpecs(ctx context.Context, pathSpecs []string) ([]reviewsweep.ReportSummary, error) {
	configPaths, err := ResolveConfigPaths(pathSpecs)
	if err != nil {
		return nil, err
	}
	return s.RunConfigs(ctx, configPaths)
}

// RunConfigsAndWriteReports executes the given configs and persists each
// resulting summary as a named JSON report in outDir.
func (s *Service) RunConfigsAndWriteReports(ctx context.Context, configPaths []string, outDir string) ([]reviewsweep.ReportSummary, error) {
	summaries, err := s.RunConfigs(ctx, configPaths)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, fmt.Errorf("no review-sweep results generated")
	}
	if err := WriteReports(outDir, summaries); err != nil {
		return nil, err
	}
	return summaries, nil
}

// RunPathSpecsAndWriteReports resolves config path specs, executes the
// resulting review sweeps, and persists each as a named JSON report.
func (s *Service) RunPathSpecsAndWriteReports(ctx context.Context, pathSpecs []string, outDir string) ([]reviewsweep.ReportSummary, error) {
	configPaths, err := ResolveConfigPaths(pathSpecs)
	if err != nil {
		return nil, err
	}
	return s.RunConfigsAndWriteReports(ctx, configPaths, outDir)
}

// WriteReports persists each summary as JSON in dir.
func WriteReports(dir string, summaries []reviewsweep.ReportSummary) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir %q: %w", dir, err)
	}
	for _, summary := range summaries {
		stem := reviewsweep.ReportStem(summary)
		if err := WriteReportJSON(filepath.Join(dir, stem+".json"), summary); err != nil {
			return fmt.Errorf("write json for %q: %w", summary.Name, err)
		}
	}
	return nil
}

// WriteReportJSON marshals s as indented JSON to path, creating parent
// directories as needed.
func WriteReportJSON(path string, s reviewsweep.ReportSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// ListReports reads persisted JSON summaries from dir and returns them in
// reverse lexical filename order (newest report name first).
func ListReports(dir string) ([]reviewsweep.ReportSummary, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob summaries in %q: %w", dir, err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))

	var summaries []reviewsweep.ReportSummary
	for _, path := range matches {
		s, err := ReadReportFile(path)
		if err != nil {
			continue
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
}

// ReadReportFile reads and parses a persisted review-sweep JSON report
// from path. Name is overridden from the file's basename (stem, minus
// ".json") rather than trusting the embedded value, since the on-disk
// stem includes the config-hash suffix (see ReportStem) that callers need
// to round-trip through ReadReportByName.
func ReadReportFile(path string) (reviewsweep.ReportSummary, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return reviewsweep.ReportSummary{}, err
	}
	var s reviewsweep.ReportSummary
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	s.Name = strings.TrimSuffix(filepath.Base(path), ".json")
	return s, nil
}

// ReadReportByName reads a persisted review-sweep JSON report from dir by
// logical report name.
func ReadReportByName(dir, name string) (reviewsweep.ReportSummary, error) {
	name = filepath.Base(name)
	if !strings.HasSuffix(name, ".json") {
		name += ".json"
	}
	return ReadReportFile(filepath.Join(dir, name))
}

func configFilesInDir(dir string) ([]string, error) {
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

func ifEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
