package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version  int         `json:"version" yaml:"version"`
	Defaults RunDefaults `json:"defaults" yaml:"defaults"`
	Runs     []RunConfig `json:"runs" yaml:"runs"`
}

type RunDefaults struct {
	StartingBalance float64 `json:"starting-balance" yaml:"starting-balance"`
	AccountCCY      string  `json:"account-ccy" yaml:"account-ccy"`
	Scale           int64   `json:"scale" yaml:"scale"`
	Strict          bool    `json:"strict" yaml:"strict"`

	RiskPct  float64 `json:"risk-pct" yaml:"risk-pct"`
	StopPips int32   `json:"stop-pips" yaml:"stop-pips"`
	TakePips int32   `json:"take-pips" yaml:"take-pips"`
	RR       float64 `json:"rr" yaml:"rr"`
	Units    int32   `json:"units" yaml:"units"`

	Source string `json:"source" yaml:"source"`
}

type RunConfig struct {
	Name     string         `json:"name" yaml:"name"`
	Data     DataConfig     `json:"data" yaml:"data"`
	Strategy StrategyConfig `json:"strategy" yaml:"strategy"`
}

type DataConfig struct {
	Source     string `json:"source" yaml:"source"`
	Instrument string `json:"instrument" yaml:"instrument"`
	Timeframe  string `json:"timeframe" yaml:"timeframe"`
	From       string `json:"from" yaml:"from"`
	To         string `json:"to" yaml:"to"`
	Strict     *bool  `json:"strict" yaml:"strict"`
}

type StrategyConfig struct {
	Kind   string         `json:"kind" yaml:"kind"`
	Params map[string]any `json:"params" yaml:"params"`
}

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

func (c *Config) runRequest(rcfg RunConfig) (*Backtest, error) {
	fmt.Printf("rcfg: %+v\n", rcfg)

	tr, err := timeRangeFromStrings(rcfg.Data.To, rcfg.Data.From, rcfg.Data.Timeframe)
	if err != nil {
		return nil, err
	}

	btr := &Backtest{
		BacktestRequest: &BacktestRequest{
			Name:       "",
			Instrument: rcfg.Data.Instrument,
			TimeRange:  tr,
		},
	}
	return btr, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func percentToRate(pct float64) Rate {
	return RateFromFloat(pct / 100.0)
}

func parseDateStart(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

func parseDateEndExclusive(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return t.AddDate(0, 0, 1), nil
}

func parseTimeframe(s string) (Timeframe, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "M1":
		return M1, nil
	case "H1":
		return H1, nil
	case "D1":
		return D1, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe %q", s)
	}
}

func getInt32Param(m map[string]any, key string) (int32, bool, error) {
	v, ok := m[key]
	if !ok {
		return 0, false, nil
	}

	switch x := v.(type) {
	case int:
		return int32(x), true, nil
	case int32:
		return x, true, nil
	case int64:
		return int32(x), true, nil
	case float64:
		return int32(x), true, nil
	default:
		return 0, true, fmt.Errorf("param %q must be numeric, got %T", key, v)
	}
}

func getFloat64Param(m map[string]any, key string) (float64, bool, error) {
	v, ok := m[key]
	if !ok {
		return 0, false, nil
	}

	switch x := v.(type) {
	case float64:
		return x, true, nil
	case float32:
		return float64(x), true, nil
	case int:
		return float64(x), true, nil
	case int32:
		return float64(x), true, nil
	case int64:
		return float64(x), true, nil
	default:
		return 0, true, fmt.Errorf("param %q must be numeric, got %T", key, v)
	}
}
