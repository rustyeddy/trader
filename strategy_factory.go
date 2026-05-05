package trader

import (
	"fmt"
	"strings"
)

func GetStrategy(name string) (Strategy, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	switch name {
	case "", "fake":
		return &Fake{
			CandleCount: 10,
		}, nil

	case "fake-02":
		return &Fake02{
			WaitBars: 8,
			HoldBars: 6,
		}, nil

	case "noop", "no-op":
		return noopStrategy{}, nil

	// case "ema-cross":
	// 	cfg, err := BuildEMACrossConfigFromRun(r)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	return NewEMACross(cfg), nil

	// case "ema-cross-adx":
	// 	cfg, err := BuildEMACrossADXConfigFromRun(r)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	return NewEMACrossADX(cfg), nil

	// case "template":
	// 	cfg, err := BuildTemplateStrategyConfigFromRun(r)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	return NewTemplateStrategy(cfg), nil

	default:
		return nil, fmt.Errorf("unsupported strategy.kind %q", name)
	}

}

func BuildEMACrossConfigFromRun(r *BacktestRun) (EMACrossConfig, error) {
	// fast, ok, err := runIntParam(r.Strategy.Params, "fast")
	// if err != nil {
	// 	return EMACrossConfig{}, err
	// }
	// if !ok || fast <= 0 {
	// 	return EMACrossConfig{}, fmt.Errorf("missing or invalid param %q", "fast")
	// }

	// slow, ok, err := runIntParam(r.Strategy.Params, "slow")
	// if err != nil {
	// 	return EMACrossConfig{}, err
	// }
	// if !ok || slow <= 0 {
	// 	return EMACrossConfig{}, fmt.Errorf("missing or invalid param %q", "slow")
	// }

	// if fast >= slow {
	// 	return EMACrossConfig{}, fmt.Errorf("fast must be less than slow (got fast=%d slow=%d)", fast, slow)
	// }

	// return EMACrossConfig{
	// 	StrategyBaseConfig: StrategyBaseConfig{Instrument: r.Instrument},
	// 	FastPeriod:         fast,
	// 	SlowPeriod:         slow,
	// 	Scale:              runScaleOrDefault(r.Scale),
	// }, nil
	return EMACrossConfig{}, nil
}

func BuildEMACrossADXConfigFromRun(r *BacktestRun) (EMACrossADXConfig, error) {
	cfg := EMACrossADXConfig{}

	// fast, ok, err := runIntParam(r.Strategy.Params, "fast")
	// if err != nil {
	// 	return cfg, err
	// }
	// if !ok || fast <= 0 {
	// 	return cfg, fmt.Errorf("missing or invalid param %q", "fast")
	// }

	// slow, ok, err := runIntParam(r.Strategy.Params, "slow")
	// if err != nil {
	// 	return cfg, err
	// }
	// if !ok || slow <= 0 {
	// 	return cfg, fmt.Errorf("missing or invalid param %q", "slow")
	// }

	// adxPeriod, ok, err := runIntParam(r.Strategy.Params, "adx_period")
	// if err != nil {
	// 	return cfg, err
	// }
	// if !ok || adxPeriod <= 0 {
	// 	adxPeriod = 14
	// }

	// adxThreshold, ok, err := runFloatParam(r.Strategy.Params, "adx_threshold")
	// if err != nil {
	// 	return cfg, err
	// }
	// if !ok || adxThreshold <= 0 {
	// 	adxThreshold = 20.0
	// }

	// minSpread, ok, err := runFloatParam(r.Strategy.Params, "min_spread")
	// if err != nil {
	// 	return cfg, err
	// }
	// if !ok {
	// 	minSpread = 0
	// }

	// requireDI, ok, err := runBoolParam(r.Strategy.Params, "require_di")
	// if err != nil {
	// 	return cfg, err
	// }
	// if !ok {
	// 	requireDI = false
	// }

	// requireADXReady, ok, err := runBoolParam(r.Strategy.Params, "require_adx_ready")
	// if err != nil {
	// 	return cfg, err
	// }
	// if !ok {
	// 	requireADXReady = true
	// }

	// return EMACrossADXConfig{
	// 	StrategyBaseConfig: StrategyBaseConfig{Instrument: r.Instrument},
	// 	FastPeriod:         fast,
	// 	SlowPeriod:         slow,
	// 	ADXPeriod:          adxPeriod,
	// 	Scale:              runScaleOrDefault(r.Scale),
	// 	MinSpread:          minSpread,
	// 	ADXThreshold:       adxThreshold,
	// 	RequireDI:          requireDI,
	// 	RequireADXReady:    requireADXReady,
	// }, nil
	return cfg, nil
}

func BuildTemplateStrategyConfigFromRun(r *BacktestRun) (TemplateStrategyConfig, error) {
	// lookback, ok, err := runIntParam(r.Strategy.Params, "lookback")
	// if err != nil {
	// 	return TemplateStrategyConfig{}, err
	// }
	// if !ok || lookback <= 0 {
	// 	return TemplateStrategyConfig{}, fmt.Errorf("missing or invalid param %q", "lookback")
	// }

	// threshold, ok, err := runFloatParam(r.Strategy.Params, "threshold")
	// if err != nil {
	// 	return TemplateStrategyConfig{}, err
	// }
	// if !ok {
	// 	return TemplateStrategyConfig{}, fmt.Errorf("missing param %q", "threshold")
	// }

	// return TemplateStrategyConfig{
	// 	StrategyBaseConfig: StrategyBaseConfig{Instrument: r.Instrument},
	// 	Lookback:           lookback,
	// 	Threshold:          threshold,
	// 	Scale:              runScaleOrDefault(r.Scale),
	// }, nil
	return TemplateStrategyConfig{}, nil
}

func runScaleOrDefault(scale Scale6) Scale6 {
	if scale <= 0 {
		return PriceScale
	}
	return scale
}

func runIntParam(m map[string]any, key string) (int, bool, error) {
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

func runFloatParam(m map[string]any, key string) (float64, bool, error) {
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

func runBoolParam(m map[string]any, key string) (bool, bool, error) {
	v, ok := m[key]
	if !ok {
		return false, false, nil
	}

	x, ok := v.(bool)
	if !ok {
		return false, true, fmt.Errorf("param %q must be bool, got %T", key, v)
	}
	return x, true, nil
}
