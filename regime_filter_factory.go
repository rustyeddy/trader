package trader

import "fmt"

// RegimeConfig mirrors the regime: section of a YAML backtest config.
type RegimeConfig struct {
	Kind   string         `json:"kind"   yaml:"kind"`
	Params map[string]any `json:"params" yaml:"params"`
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

	default:
		return nil, fmt.Errorf("unknown regime filter %q", cfg.Kind)
	}
}
