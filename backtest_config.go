package trader

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Version  int         `json:"version" yaml:"version"`
	Defaults RunDefaults `json:"defaults" yaml:"defaults"`
	Runs     []RunConfig `json:"runs" yaml:"runs"`
}

type RunDefaults struct {
	StartingBalance float64 `json:"starting_balance" yaml:"starting_balance"`
	AccountCCY      string  `json:"account_ccy" yaml:"account_ccy"`
	Scale           int64   `json:"scale" yaml:"scale"`
	Strict          bool    `json:"strict" yaml:"strict"`

	RiskPct  float64 `json:"risk_pct" yaml:"risk_pct"`
	StopPips int32   `json:"stop_pips" yaml:"stop_pips"`
	TakePips int32   `json:"take_pips" yaml:"take_pips"`
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

type ResolvedRun struct {
	Name            string
	Source          string
	Instrument      string
	Timeframe       string
	From            string
	To              string
	Strict          bool
	StartingBalance Money
	AccountCCY      string
	Scale           Scale6

	RiskPct  Rate
	StopPips Price
	TakePips Price
	RR       Rate
	Units    Units

	Strategy StrategyConfig
}

func (c *Config) ResolveRun(name string) (*ResolvedRun, error) {
	for _, run := range c.Runs {
		if run.Name == name {
			return c.resolve(run)
		}
	}
	return nil, fmt.Errorf("run %q not found", name)
}

func (c *Config) ResolveAllRuns() ([]ResolvedRun, error) {
	out := make([]ResolvedRun, 0, len(c.Runs))
	for _, run := range c.Runs {
		rr, err := c.resolve(run)
		if err != nil {
			return nil, fmt.Errorf("resolve run %q: %w", run.Name, err)
		}
		out = append(out, *rr)
	}
	return out, nil
}

func (c *Config) resolve(run RunConfig) (*ResolvedRun, error) {
	if strings.TrimSpace(run.Name) == "" {
		return nil, fmt.Errorf("missing run name")
	}
	if strings.TrimSpace(run.Data.Instrument) == "" {
		return nil, fmt.Errorf("run %q missing data.instrument", run.Name)
	}
	if strings.TrimSpace(run.Data.Timeframe) == "" {
		return nil, fmt.Errorf("run %q missing data.timeframe", run.Name)
	}
	if strings.TrimSpace(run.Data.From) == "" {
		return nil, fmt.Errorf("run %q missing data.from", run.Name)
	}
	if strings.TrimSpace(run.Data.To) == "" {
		return nil, fmt.Errorf("run %q missing data.to", run.Name)
	}
	if strings.TrimSpace(run.Strategy.Kind) == "" {
		return nil, fmt.Errorf("run %q missing strategy.kind", run.Name)
	}

	scale := Scale6(c.Defaults.Scale)
	if scale <= 0 {
		scale = PriceScale
	}

	rr := &ResolvedRun{
		Name:            strings.TrimSpace(run.Name),
		Source:          firstNonEmpty(run.Data.Source, c.Defaults.Source),
		Instrument:      strings.ToUpper(strings.TrimSpace(run.Data.Instrument)),
		Timeframe:       strings.ToUpper(strings.TrimSpace(run.Data.Timeframe)),
		From:            strings.TrimSpace(run.Data.From),
		To:              strings.TrimSpace(run.Data.To),
		Strict:          c.Defaults.Strict,
		StartingBalance: MoneyFromFloat(c.Defaults.StartingBalance),
		AccountCCY:      strings.TrimSpace(c.Defaults.AccountCCY),
		Scale:           scale,
		RiskPct:         percentToRate(c.Defaults.RiskPct),
		StopPips:        Price(c.Defaults.StopPips),
		TakePips:        Price(c.Defaults.TakePips),
		RR:              RateFromFloat(c.Defaults.RR),
		Units:           Units(c.Defaults.Units),
		Strategy:        run.Strategy,
	}

	if run.Data.Strict != nil {
		rr.Strict = *run.Data.Strict
	}

	if err := rr.ApplyCommonParamOverrides(); err != nil {
		return nil, fmt.Errorf("run %q params: %w", run.Name, err)
	}

	return rr, nil
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

func (r ResolvedRun) CandleRequest() (CandleRequest, error) {
	tf, err := parseTimeframe(r.Timeframe)
	if err != nil {
		return CandleRequest{}, err
	}

	start, err := parseDateStart(r.From)
	if err != nil {
		return CandleRequest{}, fmt.Errorf("bad from %q: %w", r.From, err)
	}

	end, err := parseDateEndExclusive(r.To)
	if err != nil {
		return CandleRequest{}, fmt.Errorf("bad to %q: %w", r.To, err)
	}

	return CandleRequest{
		Source:     r.Source,
		Instrument: r.Instrument,
		Timeframe:  tf,
		Range: TimeRange{
			Start: FromTime(start),
			End:   FromTime(end),
		},
		Strict: r.Strict,
	}, nil
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

func (r *ResolvedRun) ApplyCommonParamOverrides() error {
	if len(r.Strategy.Params) == 0 {
		return nil
	}

	if v, ok, err := getInt32Param(r.Strategy.Params, "units"); err != nil {
		return err
	} else if ok {
		r.Units = Units(v)
	}

	if v, ok, err := getInt32Param(r.Strategy.Params, "stop_pips"); err != nil {
		return err
	} else if ok {
		r.StopPips = Price(v)
	}

	if v, ok, err := getInt32Param(r.Strategy.Params, "take_pips"); err != nil {
		return err
	} else if ok {
		r.TakePips = Price(v)
	}

	if v, ok, err := getFloat64Param(r.Strategy.Params, "risk_pct"); err != nil {
		return err
	} else if ok {
		r.RiskPct = percentToRate(v)
	}

	if v, ok, err := getFloat64Param(r.Strategy.Params, "rr"); err != nil {
		return err
	} else if ok {
		r.RR = RateFromFloat(v)
	}

	return nil
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
