package trader

import "fmt"

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
	names := make([]string, len(c.filters))
	for i, f := range c.filters {
		names[i] = f.Name()
	}
	return fmt.Sprintf("Composite(%s)", joinStrings(names, "+"))
}

func (c *CompositeRegimeFilter) Ready() bool {
	for _, f := range c.filters {
		if !f.Ready() {
			return false
		}
	}
	return true
}

func (c *CompositeRegimeFilter) Tick(ct CandleTime) {
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

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// GetRegimeFilter constructs a RegimeFilter from cfg.
// If cfg.Kind is empty, NoopRegime is returned (no filtering).
func GetRegimeFilter(cfg RegimeConfig, scale Scale6) (RegimeFilter, error) {
	switch cfg.Kind {
	case "", "noop":
		return NoopRegime{}, nil

	case "choppiness":
		period, _, err := getInt32Param(cfg.Params, "period")
		if err != nil {
			return nil, err
		}
		if period <= 0 {
			period = 14
		}
		threshold, _, err := getFloat64Param(cfg.Params, "threshold")
		if err != nil {
			return nil, err
		}
		if threshold <= 0 {
			threshold = 61.8
		}
		return NewChoppinessFilter(int(period), threshold, scale), nil

	case "choppiness-d1":
		period, _, err := getInt32Param(cfg.Params, "period")
		if err != nil {
			return nil, err
		}
		if period <= 0 {
			period = 14
		}
		threshold, _, err := getFloat64Param(cfg.Params, "threshold")
		if err != nil {
			return nil, err
		}
		if threshold <= 0 {
			threshold = 61.8
		}
		return NewD1ChoppinessFilter(int(period), threshold, scale), nil

	case "session":
		start, _, err := getInt32Param(cfg.Params, "session_start")
		if err != nil {
			return nil, err
		}
		if start <= 0 {
			start = 7
		}
		end, _, err := getInt32Param(cfg.Params, "session_end")
		if err != nil {
			return nil, err
		}
		if end <= 0 {
			end = 17
		}
		return NewSessionFilter(int(start), int(end)), nil

	case "adx-d1":
		period, _, err := getInt32Param(cfg.Params, "period")
		if err != nil {
			return nil, err
		}
		if period <= 0 {
			period = 14
		}
		threshold, _, err := getFloat64Param(cfg.Params, "threshold")
		if err != nil {
			return nil, err
		}
		if threshold <= 0 {
			threshold = 20.0
		}
		return NewD1ADXFilter(int(period), threshold, scale), nil

	case "composite":
		if len(cfg.Filters) == 0 {
			return NoopRegime{}, nil
		}
		sub := make([]RegimeFilter, 0, len(cfg.Filters))
		for _, fc := range cfg.Filters {
			f, err := GetRegimeFilter(fc, scale)
			if err != nil {
				return nil, fmt.Errorf("composite sub-filter: %w", err)
			}
			sub = append(sub, f)
		}
		return NewCompositeRegimeFilter(sub), nil

	default:
		return nil, fmt.Errorf("unknown regime filter %q", cfg.Kind)
	}
}
