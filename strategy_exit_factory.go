package trader

import "fmt"

// ExitConfig mirrors the exit: section of a YAML backtest config.
type ExitConfig struct {
	Kind   string         `json:"kind"   yaml:"kind"`
	Params map[string]any `json:"params" yaml:"params"`
}

// GetExitStrategy constructs an ExitStrategy from cfg.
// If cfg.Kind is empty, NoopExit is returned (pass-through).
func GetExitStrategy(cfg ExitConfig, scale Scale6) (ExitStrategy, error) {
	switch cfg.Kind {
	case "", "noop":
		return NoopExit{}, nil

	case "chandelier":
		period, _, err := GetInt32Param(cfg.Params, "atr_period")
		if err != nil {
			return nil, err
		}
		if period <= 0 {
			period = 14
		}
		mult, _, err := GetFloat64Param(cfg.Params, "multiplier")
		if err != nil {
			return nil, err
		}
		if mult <= 0 {
			mult = 2.0
		}
		return NewChandelierExit(int(period), mult, scale)

	default:
		return nil, fmt.Errorf("unknown exit strategy %q", cfg.Kind)
	}
}
