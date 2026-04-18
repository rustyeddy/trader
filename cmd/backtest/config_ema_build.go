package backtest

import (
	"fmt"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/strategies"
)

type EMAParams struct {
	Fast int
	Slow int
}

func BuildEMACrossConfig(r trader.ResolvedRun) (strategies.EMACrossConfig, error) {
	ema, err := emaParamsFromRun(r)
	if err != nil {
		return strategies.EMACrossConfig{}, err
	}

	return strategies.EMACrossConfig{
		StrategyConfig: strategies.StrategyConfig{},
		FastPeriod:     ema.Fast,
		SlowPeriod:     ema.Slow,
		Scale:          r.Scale,
	}, nil
}

func emaParamsFromRun(r trader.ResolvedRun) (EMAParams, error) {
	fast, ok, err := getEMAIntParam(r.Strategy.Params, "fast")
	if err != nil {
		return EMAParams{}, err
	}
	if !ok || fast <= 0 {
		return EMAParams{}, fmt.Errorf("missing or invalid param %q", "fast")
	}

	slow, ok, err := getEMAIntParam(r.Strategy.Params, "slow")
	if err != nil {
		return EMAParams{}, err
	}
	if !ok || slow <= 0 {
		return EMAParams{}, fmt.Errorf("missing or invalid param %q", "slow")
	}

	if fast >= slow {
		return EMAParams{}, fmt.Errorf("fast must be less than slow (got fast=%d slow=%d)", fast, slow)
	}

	return EMAParams{
		Fast: fast,
		Slow: slow,
	}, nil
}

func getEMAIntParam(m map[string]any, key string) (int, bool, error) {
	v, ok := m[key]
	if !ok {
		return 0, false, nil
	}

	switch x := v.(type) {
	case int:
		return x, true, nil
	case int32:
		return int(x), true, nil
	case int64:
		return int(x), true, nil
	case float64:
		return int(x), true, nil
	default:
		return 0, true, fmt.Errorf("param %q must be numeric, got %T", key, v)
	}
}
