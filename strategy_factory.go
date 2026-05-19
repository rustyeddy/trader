package trader

import (
	"fmt"
	"strings"
)

func GetStrategy(scfg StrategyConfig) (Strategy, error) {
	name := strings.ToLower(strings.TrimSpace(scfg.Kind))
	params := scfg.Params

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

	case "lifecycle-test":
		return &LifecycleTestStrategy{
			Units:    1000,
			StopPips: 20,
		}, nil

	case "template":
		return NewTemplateStrategy(TemplateStrategyConfig{
			StrategyBaseConfig: StrategyBaseConfig{},
			Lookback:           5,
			Threshold:          0.0015,
			Scale:              PriceScale,
		}), nil

	case "ema-cross":
		fast, ok, err := getInt32Param(params, "fast")
		if err != nil {
			return nil, err
		}
		if !ok || fast <= 0 {
			return nil, fmt.Errorf("ema-cross: missing or invalid param %q", "fast")
		}
		slow, ok, err := getInt32Param(params, "slow")
		if err != nil {
			return nil, err
		}
		if !ok || slow <= 0 {
			return nil, fmt.Errorf("ema-cross: missing or invalid param %q", "slow")
		}
		if fast >= slow {
			return nil, fmt.Errorf("ema-cross: fast (%d) must be < slow (%d)", fast, slow)
		}
		stopPips, _, err := getFloat64Param(params, "stop_pips")
		if err != nil {
			return nil, err
		}
		minSpread, _, err := getFloat64Param(params, "min_spread")
		if err != nil {
			return nil, err
		}
		atrPeriod, _, err := getInt32Param(params, "atr_period")
		if err != nil {
			return nil, err
		}
		atrMult, _, err := getFloat64Param(params, "atr_multiplier")
		if err != nil {
			return nil, err
		}
		return NewEMACross(EMACrossConfig{
			FastPeriod:    int(fast),
			SlowPeriod:    int(slow),
			Scale:         PriceScale,
			StopPips:      pipsFromFloat(stopPips),
			MinSpread:     minSpread,
			ATRPeriod:     int(atrPeriod),
			ATRMultiplier: atrMult,
		}), nil

	case "ema-cross-adx":
		fast, ok, err := getInt32Param(params, "fast")
		if err != nil {
			return nil, err
		}
		if !ok || fast <= 0 {
			return nil, fmt.Errorf("ema-cross-adx: missing or invalid param %q", "fast")
		}
		slow, ok, err := getInt32Param(params, "slow")
		if err != nil {
			return nil, err
		}
		if !ok || slow <= 0 {
			return nil, fmt.Errorf("ema-cross-adx: missing or invalid param %q", "slow")
		}
		if fast >= slow {
			return nil, fmt.Errorf("ema-cross-adx: fast (%d) must be < slow (%d)", fast, slow)
		}
		adxPeriod, _, err := getInt32Param(params, "adx_period")
		if err != nil {
			return nil, err
		}
		if adxPeriod <= 0 {
			adxPeriod = 14
		}
		adxThreshold, _, err := getFloat64Param(params, "adx_threshold")
		if err != nil {
			return nil, err
		}
		if adxThreshold <= 0 {
			adxThreshold = 20.0
		}
		stopPips, _, err := getFloat64Param(params, "stop_pips")
		if err != nil {
			return nil, err
		}
		minSpread, _, err := getFloat64Param(params, "min_spread")
		if err != nil {
			return nil, err
		}
		atrPeriod, _, err := getInt32Param(params, "atr_period")
		if err != nil {
			return nil, err
		}
		atrMult, _, err := getFloat64Param(params, "atr_multiplier")
		if err != nil {
			return nil, err
		}
		requireDI, _, err := runBoolParam(params, "require_di")
		if err != nil {
			return nil, err
		}
		requireADXReady, ok, err := runBoolParam(params, "require_adx_ready")
		if err != nil {
			return nil, err
		}
		if !ok {
			requireADXReady = true
		}
		return NewEMACrossADX(EMACrossADXConfig{
			FastPeriod:      int(fast),
			SlowPeriod:      int(slow),
			ADXPeriod:       int(adxPeriod),
			Scale:           PriceScale,
			StopPips:        pipsFromFloat(stopPips),
			MinSpread:       minSpread,
			ATRPeriod:       int(atrPeriod),
			ATRMultiplier:   atrMult,
			ADXThreshold:    adxThreshold,
			RequireDI:       requireDI,
			RequireADXReady: requireADXReady,
		}), nil

	default:
		return nil, fmt.Errorf("unsupported strategy.kind %q", name)
	}

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
