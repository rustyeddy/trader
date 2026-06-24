package strategy

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// GetIntParam
// ---------------------------------------------------------------------------

func TestGetIntParam_Missing(t *testing.T) {
	t.Parallel()

	v, ok, err := GetIntParam(map[string]any{}, "n")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, 0, v)
}

func TestGetIntParam_Int(t *testing.T) {
	t.Parallel()

	v, ok, err := GetIntParam(map[string]any{"n": 42}, "n")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 42, v)
}

func TestGetIntParam_Int32(t *testing.T) {
	t.Parallel()

	v, ok, err := GetIntParam(map[string]any{"n": int32(7)}, "n")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 7, v)
}

func TestGetIntParam_Int64(t *testing.T) {
	t.Parallel()

	v, ok, err := GetIntParam(map[string]any{"n": int64(99)}, "n")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 99, v)
}

func TestGetIntParam_Float64Integral(t *testing.T) {
	t.Parallel()

	v, ok, err := GetIntParam(map[string]any{"n": float64(5)}, "n")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 5, v)
}

func TestGetIntParam_Float64NonIntegral(t *testing.T) {
	t.Parallel()

	_, ok, err := GetIntParam(map[string]any{"n": 3.14}, "n")
	require.Error(t, err)
	require.True(t, ok)
	require.Contains(t, err.Error(), "must be an integer")
}

func TestGetIntParam_Float64Overflow(t *testing.T) {
	t.Parallel()

	_, ok, err := GetIntParam(map[string]any{"n": 1e20}, "n")
	require.Error(t, err)
	require.True(t, ok)
	require.Contains(t, err.Error(), "overflows")
}

func TestGetIntParam_WrongType(t *testing.T) {
	t.Parallel()

	_, ok, err := GetIntParam(map[string]any{"n": "not-a-number"}, "n")
	require.Error(t, err)
	require.True(t, ok)
	require.Contains(t, err.Error(), "must be numeric")
}

// ---------------------------------------------------------------------------
// GetInt32Param
// ---------------------------------------------------------------------------

func TestGetInt32Param_Missing(t *testing.T) {
	t.Parallel()

	v, ok, err := GetInt32Param(map[string]any{}, "n")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, int32(0), v)
}

func TestGetInt32Param_Valid(t *testing.T) {
	t.Parallel()

	v, ok, err := GetInt32Param(map[string]any{"n": int32(100)}, "n")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int32(100), v)
}

func TestGetInt32Param_Overflow(t *testing.T) {
	t.Parallel()

	// int64 value that exceeds int32 max
	big := float64(math.MaxInt32) + 1
	_, ok, err := GetInt32Param(map[string]any{"n": big}, "n")
	require.Error(t, err)
	require.True(t, ok)
	require.Contains(t, err.Error(), "overflows int32")
}

func TestGetInt32Param_WrongType(t *testing.T) {
	t.Parallel()

	_, ok, err := GetInt32Param(map[string]any{"n": "bad"}, "n")
	require.Error(t, err)
	require.True(t, ok)
}

// ---------------------------------------------------------------------------
// GetFloat64Param
// ---------------------------------------------------------------------------

func TestGetFloat64Param_Missing(t *testing.T) {
	t.Parallel()

	v, ok, err := GetFloat64Param(map[string]any{}, "x")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, float64(0), v)
}

func TestGetFloat64Param_Float64(t *testing.T) {
	t.Parallel()

	v, ok, err := GetFloat64Param(map[string]any{"x": 1.5}, "x")
	require.NoError(t, err)
	require.True(t, ok)
	require.InDelta(t, 1.5, v, 1e-9)
}

func TestGetFloat64Param_Float32(t *testing.T) {
	t.Parallel()

	v, ok, err := GetFloat64Param(map[string]any{"x": float32(2.5)}, "x")
	require.NoError(t, err)
	require.True(t, ok)
	require.InDelta(t, 2.5, v, 1e-6)
}

func TestGetFloat64Param_Int(t *testing.T) {
	t.Parallel()

	v, ok, err := GetFloat64Param(map[string]any{"x": 10}, "x")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, float64(10), v)
}

func TestGetFloat64Param_Int32(t *testing.T) {
	t.Parallel()

	v, ok, err := GetFloat64Param(map[string]any{"x": int32(3)}, "x")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, float64(3), v)
}

func TestGetFloat64Param_Int64(t *testing.T) {
	t.Parallel()

	v, ok, err := GetFloat64Param(map[string]any{"x": int64(7)}, "x")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, float64(7), v)
}

func TestGetFloat64Param_WrongType(t *testing.T) {
	t.Parallel()

	_, ok, err := GetFloat64Param(map[string]any{"x": "oops"}, "x")
	require.Error(t, err)
	require.True(t, ok)
	require.Contains(t, err.Error(), "must be numeric")
}

// ---------------------------------------------------------------------------
// GetBoolParam
// ---------------------------------------------------------------------------

func TestGetBoolParam_Missing(t *testing.T) {
	t.Parallel()

	v, ok, err := GetBoolParam(map[string]any{}, "flag")
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, v)
}

func TestGetBoolParam_True(t *testing.T) {
	t.Parallel()

	v, ok, err := GetBoolParam(map[string]any{"flag": true}, "flag")
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, v)
}

func TestGetBoolParam_False(t *testing.T) {
	t.Parallel()

	v, ok, err := GetBoolParam(map[string]any{"flag": false}, "flag")
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, v)
}

func TestGetBoolParam_WrongType(t *testing.T) {
	t.Parallel()

	_, ok, err := GetBoolParam(map[string]any{"flag": "yes"}, "flag")
	require.Error(t, err)
	require.True(t, ok)
	require.Contains(t, err.Error(), "must be bool")
}

// ---------------------------------------------------------------------------
// GetStringParam
// ---------------------------------------------------------------------------

func TestGetStringParam_Missing(t *testing.T) {
	t.Parallel()

	v, ok, err := GetStringParam(map[string]any{}, "s")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, "", v)
}

func TestGetStringParam_Valid(t *testing.T) {
	t.Parallel()

	v, ok, err := GetStringParam(map[string]any{"s": "hello"}, "s")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "hello", v)
}

func TestGetStringParam_WrongType(t *testing.T) {
	t.Parallel()

	_, ok, err := GetStringParam(map[string]any{"s": 42}, "s")
	require.Error(t, err)
	require.True(t, ok)
	require.Contains(t, err.Error(), "must be a string")
}
