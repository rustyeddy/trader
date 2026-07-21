// Package reviewsweep is the config/report data model for historical
// review sweeps: replaying review.ReviewPair's classification over a date
// range and persisting the result as a named report. It mirrors the
// top-level backtest package's Config/report shape (see backtest/config.go)
// so review sweeps are configured, run, and browsed the same way backtests
// are — YAML config in, named JSON report out.
package reviewsweep

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rustyeddy/trader/review"
	"gopkg.in/yaml.v3"
)

// Config is the top-level structure parsed from a YAML or JSON review-sweep
// config file.
type Config struct {
	Version int         `json:"version" yaml:"version"`
	Runs    []RunConfig `json:"runs" yaml:"runs"`
}

// RunConfig describes a single historical sweep: which instruments to
// replay, over what date range, at what step interval.
type RunConfig struct {
	Name        string   `json:"name" yaml:"name"`
	Instruments []string `json:"instruments" yaml:"instruments"`
	From        string   `json:"from" yaml:"from"` // YYYY-MM-DD, inclusive
	To          string   `json:"to" yaml:"to"`     // YYYY-MM-DD, inclusive

	// Interval is a Go duration string between sweep steps (e.g. "24h",
	// "72h"). Defaults to "24h" when blank.
	Interval string `json:"interval" yaml:"interval"`

	// Thresholds overrides review.DefaultThresholds() field-by-field; a
	// zero-valued field falls back to the default.
	Thresholds review.Thresholds `json:"thresholds" yaml:"thresholds"`
}

// LoadConfig reads and parses a YAML or JSON review-sweep config file from
// path. The file extension determines the parser.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := &Config{}
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("parse yaml %q: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("parse json %q: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("unsupported config extension %q (use .yaml, .yml, or .json)", ext)
	}

	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if len(cfg.Runs) == 0 {
		return nil, fmt.Errorf("config %q has no runs", path)
	}
	return cfg, nil
}

// ReportSummary is one persisted review-sweep report: the run config used
// plus every (step, instrument) classification produced by replaying it.
type ReportSummary struct {
	Name        string   `json:"name"`
	ConfigHash  string   `json:"config_hash"` // 8-char SHA256 prefix of the run config
	Instruments []string `json:"instruments"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	Interval    string   `json:"interval"`
	GeneratedAt time.Time `json:"generated_at"`

	Results []review.ReviewResult `json:"results"`
}

// HashRunConfig returns the first 8 hex characters of the SHA256 of the
// run config's execution-affecting fields (everything but Name). Used as a
// stable filename suffix: identical sweep inputs hash to the same file.
func HashRunConfig(cfg RunConfig) string {
	type hashable struct {
		Instruments []string          `json:"instruments"`
		From        string            `json:"from"`
		To          string            `json:"to"`
		Interval    string            `json:"interval"`
		Thresholds  review.Thresholds `json:"thresholds"`
	}
	b, _ := json.Marshal(hashable{
		Instruments: cfg.Instruments,
		From:        cfg.From,
		To:          cfg.To,
		Interval:    cfg.Interval,
		Thresholds:  cfg.Thresholds,
	})
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:4])
}

// ReportStem returns the canonical report filename stem (without
// extension) for a summary: name plus its config hash.
func ReportStem(summary ReportSummary) string {
	hash := strings.TrimSpace(summary.ConfigHash)
	if hash == "" {
		return summary.Name
	}
	return summary.Name + "-" + hash
}
