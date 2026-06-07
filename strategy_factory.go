package trader

import "fmt"

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
	return getIntParam(m, key)
}

func runFloatParam(m map[string]any, key string) (float64, bool, error) {
	return getFloat64Param(m, key)
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
