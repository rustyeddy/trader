package types

import (
	"fmt"
	"math"
)

func requireIntegralFloatParam(key string, v float64) error {
	if math.Trunc(v) != v {
		return fmt.Errorf("param %q must be an integer, got %v", key, v)
	}
	return nil
}

func requireIntRangeParam(key string, v int64) error {
	if v < math.MinInt || v > math.MaxInt {
		return fmt.Errorf("param %q=%d overflows int", key, v)
	}
	return nil
}

func requireFloatIntRangeParam(key string, v float64, min, max int64, kind string) error {
	if v < float64(min) || v > float64(max) {
		return fmt.Errorf("param %q=%v overflows %s", key, v, kind)
	}
	return nil
}

func requireInt32RangeParam(key string, v int64) error {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return fmt.Errorf("param %q=%d overflows int32", key, v)
	}
	return nil
}

// GetIntParam extracts an int from a params map, accepting the numeric types
// produced by YAML/JSON decoding. Returns (0, false, nil) when the key is
// absent, or an error if the value is not numeric.
func GetIntParam(m map[string]any, key string) (int, bool, error) {
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
		if err := requireIntRangeParam(key, x); err != nil {
			return 0, true, err
		}
		return int(x), true, nil
	case float64:
		if err := requireIntegralFloatParam(key, x); err != nil {
			return 0, true, err
		}
		if err := requireFloatIntRangeParam(key, x, math.MinInt, math.MaxInt, "int"); err != nil {
			return 0, true, err
		}
		return int(int64(x)), true, nil
	default:
		return 0, true, fmt.Errorf("param %q must be numeric, got %T", key, v)
	}
}

// GetInt32Param extracts an int32 from a params map, accepting the numeric
// types produced by YAML/JSON decoding. Returns (0, false, nil) when the key
// is absent, or an error if the value is not numeric.
func GetInt32Param(m map[string]any, key string) (int32, bool, error) {
	v, ok, err := GetIntParam(m, key)
	if err != nil || !ok {
		return 0, ok, err
	}
	if err := requireInt32RangeParam(key, int64(v)); err != nil {
		return 0, true, err
	}
	return int32(v), true, nil
}

// GetFloat64Param extracts a float64 from a params map, widening integer types
// as needed. Returns (0, false, nil) when the key is absent, or an error if
// the value is not numeric.
func GetFloat64Param(m map[string]any, key string) (float64, bool, error) {
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

// GetBoolParam extracts a bool param, or returns ok=false if missing.
func GetBoolParam(m map[string]any, key string) (bool, bool, error) {
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

// GetStringParam extracts a string param, or returns ok=false if missing.
func GetStringParam(m map[string]any, key string) (string, bool, error) {
	v, ok := m[key]
	if !ok {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", true, fmt.Errorf("param %q must be a string, got %T", key, v)
	}
	return s, true, nil
}

// GetMapParam extracts a nested map[string]any param (e.g. an entry-params:
// value embedded inside a strategy's own flat params map), or returns
// ok=false if missing. YAML/JSON decoding into map[string]any always
// produces map[string]any for a nested mapping, so no further coercion is
// needed.
func GetMapParam(m map[string]any, key string) (map[string]any, bool, error) {
	v, ok := m[key]
	if !ok {
		return nil, false, nil
	}
	x, ok := v.(map[string]any)
	if !ok {
		return nil, true, fmt.Errorf("param %q must be a map, got %T", key, v)
	}
	return x, true, nil
}
