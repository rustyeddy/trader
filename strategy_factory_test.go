package trader

import (
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

	_, ok, err = GetIntParam(m, "bad")
	assert.True(t, ok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be numeric")
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

// ema-cross and ema-cross-adx factory tests moved to ./strategies/emacross
// and ./strategies/emacrossadx respectively, along with the strategies
// themselves. The registry lookup itself is exercised by every backtest
// integration test.

func TestGetStrategy_Unknown(t *testing.T) {
	t.Parallel()

	_, err := GetStrategy(StrategyConfig{Kind: "no-such-strategy"})
	require.Error(t, err)
}

