package trader

import (
	"fmt"
)

// All concrete strategies live in ./strategies/<name>/ packages and register
// themselves via init(). Trigger registration with blank imports in cmd/main
// or wherever the binary is built.

// ─── helper functions used by strategy implementations and re-exported via
// strategy_registry.go for use from sibling packages ───

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
