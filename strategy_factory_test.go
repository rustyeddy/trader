package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunScaleOrDefault(t *testing.T) {
	t.Parallel()

	assert.Equal(t, PriceScale, runScaleOrDefault(0))
	assert.Equal(t, PriceScale, runScaleOrDefault(-1))

	customScale := Scale6(12345)
	assert.Equal(t, customScale, runScaleOrDefault(customScale))
}

func TestRunIntParam(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"int":     42,
		"int32":   int32(10),
		"int64":   int64(20),
		"float64": float64(30),
		"bad":     "string",
	}

	v, ok, err := runIntParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 0, v)

	v, ok, err = runIntParam(m, "int")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 42, v)

	v, ok, err = runIntParam(m, "int32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 10, v)

	v, ok, err = runIntParam(m, "int64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 20, v)

	v, ok, err = runIntParam(m, "float64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 30, v)

	_, ok, err = runIntParam(m, "bad")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be numeric")
}

func TestRunFloatParam(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"float64": 1.5,
		"float32": float32(2.5),
		"int":     3,
		"int32":   int32(4),
		"int64":   int64(5),
		"bad":     "string",
	}

	v, ok, err := runFloatParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, v)

	v, ok, err = runFloatParam(m, "float64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.InDelta(t, 1.5, v, 1e-9)

	v, ok, err = runFloatParam(m, "float32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.InDelta(t, 2.5, v, 1e-4)

	v, ok, err = runFloatParam(m, "int")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 3.0, v)

	v, ok, err = runFloatParam(m, "int32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 4.0, v)

	v, ok, err = runFloatParam(m, "int64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 5.0, v)

	_, ok, err = runFloatParam(m, "bad")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be numeric")
}

func TestRunBoolParam(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"true":  true,
		"false": false,
		"bad":   "string",
	}

	v, ok, err := runBoolParam(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.False(t, v)

	v, ok, err = runBoolParam(m, "true")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.True(t, v)

	v, ok, err = runBoolParam(m, "false")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.False(t, v)

	_, ok, err = runBoolParam(m, "bad")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be bool")
}

func TestGetStrategy_EMACross(t *testing.T) {
	t.Parallel()

	_, err := GetStrategy(StrategyConfig{Kind: "ema-cross", Params: map[string]any{
		"fast": 9,
		"slow": 21,
	}})
	require.NoError(t, err)

	_, err = GetStrategy(StrategyConfig{Kind: "ema-cross", Params: map[string]any{
		"fast": 21,
		"slow": 9,
	}})
	require.Error(t, err, "fast >= slow should error")

	_, err = GetStrategy(StrategyConfig{Kind: "ema-cross", Params: map[string]any{
		"slow": 21,
	}})
	require.Error(t, err, "missing fast should error")
}

func TestGetStrategy_EMACrossADX(t *testing.T) {
	t.Parallel()

	s, err := GetStrategy(StrategyConfig{Kind: "ema-cross-adx", Params: map[string]any{
		"fast":       9,
		"slow":       21,
		"adx_period": 14,
	}})
	require.NoError(t, err)
	assert.Contains(t, s.Name(), "EMA_CROSS_ADX")

	_, err = GetStrategy(StrategyConfig{Kind: "ema-cross-adx", Params: map[string]any{
		"fast": 9,
	}})
	require.Error(t, err, "missing slow should error")
}

func TestGetStrategy_Unknown(t *testing.T) {
	t.Parallel()

	_, err := GetStrategy(StrategyConfig{Kind: "no-such-strategy"})
	require.Error(t, err)
}
