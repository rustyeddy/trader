package trader

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetIntParam(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"int":     42,
		"int32":   int32(10),
		"int64":   int64(20),
		"float64": float64(30),
		"frac":    float64(30.5),
		"bad":     "string",
	}

	v, ok, err := GetIntParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 0, v)

	v, ok, err = GetIntParam(m, "int")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 42, v)

	v, ok, err = GetIntParam(m, "int32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 10, v)

	v, ok, err = GetIntParam(m, "int64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 20, v)

	v, ok, err = GetIntParam(m, "float64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 30, v)

	_, ok, err = GetIntParam(m, "frac")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an integer")

	_, ok, err = GetIntParam(m, "bad")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be numeric")
}

func TestGetIntParam_Overflow(t *testing.T) {
	t.Parallel()

	_, ok, err := GetIntParam(map[string]any{"float64": 1e20}, "float64")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overflows int")

	if maxInt < math.MaxInt64 {
		_, ok, err = GetIntParam(map[string]any{"int64": int64(maxInt) + 1}, "int64")
		assert.True(t, ok)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "overflows int")
	}
}

func TestGetBoolParam(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"true":  true,
		"false": false,
		"bad":   "string",
	}

	v, ok, err := GetBoolParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.False(t, v)

	v, ok, err = GetBoolParam(m, "true")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.True(t, v)

	v, ok, err = GetBoolParam(m, "false")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.False(t, v)

	_, ok, err = GetBoolParam(m, "bad")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be bool")
}

func TestGetStringParam(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"value": "abc",
		"bad":   123,
	}

	v, ok, err := GetStringParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, "", v)

	v, ok, err = GetStringParam(m, "value")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, "abc", v)

	_, ok, err = GetStringParam(m, "bad")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a string")
}

func TestGetParams_NilMap(t *testing.T) {
	t.Parallel()

	var m map[string]any

	vInt, ok, err := GetIntParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 0, vInt)

	vBool, ok, err := GetBoolParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.False(t, vBool)

	vString, ok, err := GetStringParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, "", vString)

	vFloat, ok, err := GetFloat64Param(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, vFloat)

	vInt32, ok, err := GetInt32Param(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), vInt32)
}

// ema-cross and ema-cross-adx factory tests moved to ./strategies/emacross
// and ./strategies/emacrossadx respectively, along with the strategies
// themselves. The registry lookup itself is exercised by every backtest
// integration test.

func TestGetStrategy_Unknown(t *testing.T) {
	t.Parallel()

	_, err := GetStrategy(StrategyConfig{Kind: "no-such-strategy"})
	require.Error(t, err)
}
