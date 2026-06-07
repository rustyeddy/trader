package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level structure parsed from a YAML or JSON config file.
// It carries a set of defaults that are merged into each RunConfig before
// the run is executed.
type Config struct {
	Version  int         `json:"version" yaml:"version"`
	Defaults RunDefaults `json:"defaults" yaml:"defaults"`
	Runs     []RunConfig `json:"runs" yaml:"runs"`
}

// RunDefaults holds account-level and execution-cost settings that apply to
// every run in the config unless overridden at the run level.
type RunDefaults struct {
	StartingBalance float64 `json:"starting-balance" yaml:"starting-balance"`
	AccountCCY      string  `json:"account-ccy" yaml:"account-ccy"`
	Scale           int64   `json:"scale" yaml:"scale"`
	Strict          bool    `json:"strict" yaml:"strict"`

	RiskPct       float64 `json:"risk-pct" yaml:"risk-pct"`
	StopPips      int32   `json:"stop-pips" yaml:"stop-pips"`
	TakePips      int32   `json:"take-pips" yaml:"take-pips"`
	RR            float64 `json:"rr" yaml:"rr"`
	Units         int32   `json:"units" yaml:"units"`
	SlippagePips  float64 `json:"slippage-pips" yaml:"slippage-pips"`
	MaxSpreadPips float64 `json:"max-spread-pips" yaml:"max-spread-pips"`

	Source string `json:"source" yaml:"source"`
}

// RunConfig describes a single backtest run: what data to load, which
// strategy to use, and optional exit and regime-filter overrides.
type RunConfig struct {
	Name     string         `json:"name"     yaml:"name"`
	Data     DataConfig     `json:"data"     yaml:"data"`
	Strategy StrategyConfig `json:"strategy" yaml:"strategy"`
	Exit     ExitConfig     `json:"exit"     yaml:"exit"`
	Regime   RegimeConfig   `json:"regime"   yaml:"regime"`
}

// DataConfig specifies the data source, instrument, timeframe, and date range
// for a run.
type DataConfig struct {
	Source     string `json:"source" yaml:"source"`
	Instrument string `json:"instrument" yaml:"instrument"`
	Timeframe  string `json:"timeframe" yaml:"timeframe"`
	From       string `json:"from" yaml:"from"`
	To         string `json:"to" yaml:"to"`
	Strict     *bool  `json:"strict" yaml:"strict"`
}

// StrategyConfig names the strategy and carries arbitrary key/value parameters
// that are passed to the strategy constructor at build time.
type StrategyConfig struct {
	Kind   string         `json:"kind" yaml:"kind"`
	Params map[string]any `json:"params" yaml:"params"`
}

// LoadConfig reads and parses a YAML or JSON config file from path.
// The file extension determines the parser (.yaml/.yml → YAML; .json → JSON).
// Returns an error if the file is missing, unparseable, or contains no runs.
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

// firstNonEmpty returns the first non-blank string from vals, or "" if all
// are blank or empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}
