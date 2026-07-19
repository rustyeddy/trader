package strategy

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// RegimeConfig mirrors the regime: section of a YAML backtest config.
type RegimeConfig struct {
	Kind    string         `json:"kind"    yaml:"kind"`
	Params  map[string]any `json:"params"  yaml:"params"`
	Filters []RegimeConfig `json:"filters" yaml:"filters"` // for composite kind
}

// CompositeRegimeFilter ANDs multiple RegimeFilters: Trending() returns true
// only when every sub-filter returns true. Tick() is called on all sub-filters
// regardless of their individual state so each indicator stays current.
type CompositeRegimeFilter struct {
	filters []RegimeFilter
}

func NewCompositeRegimeFilter(filters []RegimeFilter) *CompositeRegimeFilter {
	return &CompositeRegimeFilter{filters: filters}
}

func (c *CompositeRegimeFilter) Name() string {
	names := make([]string, 0, len(c.filters))
	for _, f := range c.filters {
		if name := strings.TrimSpace(f.Name()); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "Composite"
	}
	return fmt.Sprintf("Composite(%s)", strings.Join(names, "+"))
}

func (c *CompositeRegimeFilter) Ready() bool {
	for _, f := range c.filters {
		if !f.Ready() {
			return false
		}
	}
	return true
}

func (c *CompositeRegimeFilter) Tick(ct market.Candle) {
	for _, f := range c.filters {
		f.Tick(ct)
	}
}

func (c *CompositeRegimeFilter) Trending() bool {
	for _, f := range c.filters {
		if !f.Trending() {
			return false
		}
	}
	return true
}

func (c *CompositeRegimeFilter) AllowSide(side types.Side) bool {
	for _, f := range c.filters {
		if !f.AllowSide(side) {
			return false
		}
	}
	return true
}

// GetRegimeFilter constructs a RegimeFilter from cfg.
// If cfg.Kind is empty, NoopRegime is returned (no filtering).
func GetRegimeFilter(cfg RegimeConfig, scale types.Scale6) (RegimeFilter, error) {
	switch normalizeRegimeKind(cfg.Kind) {
	case "", "noop":
		return NoopRegime{}, nil

	case "choppiness":
		period, err := positiveIntParamOrDefault(cfg.Params, "period", 14)
		if err != nil {
			return nil, err
		}
		threshold, err := positiveFloat64ParamOrDefault(cfg.Params, "threshold", 61.8)
		if err != nil {
			return nil, err
		}
		return NewChoppinessFilter(period, threshold, scale)

	case "choppiness-d1":
		period, err := positiveIntParamOrDefault(cfg.Params, "period", 14)
		if err != nil {
			return nil, err
		}
		threshold, err := positiveFloat64ParamOrDefault(cfg.Params, "threshold", 61.8)
		if err != nil {
			return nil, err
		}
		return NewD1ChoppinessFilter(period, threshold, scale)

	case "session":
		start, end, err := sessionWindowFromParams(cfg.Params)
		if err != nil {
			return nil, err
		}
		return NewSessionFilter(start, end)

	case "adx-d1":
		period, err := positiveIntParamOrDefault(cfg.Params, "period", 14)
		if err != nil {
			return nil, err
		}
		threshold, err := float64ParamOrDefault(cfg.Params, "threshold", 20.0)
		if err != nil {
			return nil, err
		}
		return NewD1ADXFilter(period, threshold, scale)

	case "weekly-ema":
		period, err := positiveIntParamOrDefault(cfg.Params, "period", 20)
		if err != nil {
			return nil, err
		}
		return NewWeeklyEMAFilter(period, scale)

	case "atr-percentile":
		atrPeriod, err := positiveIntParamOrDefault(cfg.Params, "atr_period", 20)
		if err != nil {
			return nil, err
		}
		windowSize, err := positiveIntParamOrDefault(cfg.Params, "window_size", 200)
		if err != nil {
			return nil, err
		}
		threshold, err := float64ParamOrDefault(cfg.Params, "threshold", 20.0)
		if err != nil {
			return nil, err
		}
		return NewATRPercentileFilter(atrPeriod, windowSize, threshold, scale)

	case "composite":
		if len(cfg.Filters) == 0 {
			return nil, fmt.Errorf("composite regime requires at least one filter")
		}
		sub := make([]RegimeFilter, 0, len(cfg.Filters))
		for _, fc := range cfg.Filters {
			f, err := GetRegimeFilter(fc, scale)
			if err != nil {
				return nil, fmt.Errorf("composite sub-filter: %w", err)
			}
			sub = append(sub, f)
		}
		if len(sub) == 1 {
			return sub[0], nil
		}
		return NewCompositeRegimeFilter(sub), nil

	default:
		return nil, fmt.Errorf("unknown regime filter %q", strings.TrimSpace(cfg.Kind))
	}
}

func normalizeRegimeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func positiveIntParamOrDefault(params map[string]any, key string, def int) (int, error) {
	v, ok, err := types.GetInt32Param(params, key)
	if err != nil {
		return 0, err
	}
	if !ok || v <= 0 {
		return def, nil
	}
	return int(v), nil
}

func positiveFloat64ParamOrDefault(params map[string]any, key string, def float64) (float64, error) {
	v, ok, err := types.GetFloat64Param(params, key)
	if err != nil {
		return 0, err
	}
	if !ok || v <= 0 {
		return def, nil
	}
	return v, nil
}

func float64ParamOrDefault(params map[string]any, key string, def float64) (float64, error) {
	v, ok, err := types.GetFloat64Param(params, key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return def, nil
	}
	return v, nil
}

func sessionWindowFromParams(params map[string]any) (int, int, error) {
	start := 7
	if v, ok, err := types.GetInt32Param(params, "session_start"); err != nil {
		return 0, 0, err
	} else if ok {
		start = int(v)
	}

	end := 17
	if v, ok, err := types.GetInt32Param(params, "session_end"); err != nil {
		return 0, 0, err
	} else if ok {
		end = int(v)
	}

	if err := validateSessionWindow(start, end); err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func validateSessionWindow(start, end int) error {
	switch {
	case start < 0 || start > 23:
		return fmt.Errorf("session_start must be between 0 and 23, got %d", start)
	case end < 1 || end > 24:
		return fmt.Errorf("session_end must be between 1 and 24, got %d", end)
	case start >= end:
		return fmt.Errorf("session_start must be less than session_end, got %d >= %d", start, end)
	default:
		return nil
	}
}
