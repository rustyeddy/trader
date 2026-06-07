package trader

import "fmt"

// getIntParam extracts an int from a params map, accepting the numeric types
// produced by YAML/JSON decoding. Returns (0, false, nil) when the key is
// absent, or an error if the value is not numeric.
func getIntParam(m map[string]any, key string) (int, bool, error) {
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

// getInt32Param extracts an int32 from a params map, accepting the numeric
// types produced by YAML/JSON decoding. Returns (0, false, nil) when the key
// is absent, or an error if the value is not numeric.
func getInt32Param(m map[string]any, key string) (int32, bool, error) {
	v, ok, err := getIntParam(m, key)
	if err != nil || !ok {
		return 0, ok, err
	}
	return int32(v), true, nil
}

// getFloat64Param extracts a float64 from a params map, widening integer types
// as needed. Returns (0, false, nil) when the key is absent, or an error if
// the value is not numeric.
func getFloat64Param(m map[string]any, key string) (float64, bool, error) {
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
