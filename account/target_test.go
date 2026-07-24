package account

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/config/active"
)

func TestResolveTarget_FlagsTakePrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OANDA_ACCOUNT_ID", "env-acc")
	b, a, err := ResolveTarget("oanda", true, "flag-acc", true, "rc-acc")
	require.NoError(t, err)
	assert.Equal(t, "oanda", b)
	assert.Equal(t, "flag-acc", a)
}

func TestResolveTarget_EnvVarFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OANDA_ACCOUNT_ID", "env-acc")
	_, a, err := ResolveTarget("", false, "", false, "")
	require.NoError(t, err)
	assert.Equal(t, "env-acc", a)
}

func TestResolveTarget_RCFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OANDA_ACCOUNT_ID", "")
	_, a, err := ResolveTarget("", false, "", false, "rc-acc")
	require.NoError(t, err)
	assert.Equal(t, "rc-acc", a)
}

func TestResolveTarget_ActiveFileFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OANDA_ACCOUNT_ID", "")
	require.NoError(t, active.Save(active.Selection{Broker: "oanda", AccountID: "active-acc"}))

	b, a, err := ResolveTarget("", false, "", false, "")
	require.NoError(t, err)
	assert.Equal(t, "oanda", b)
	assert.Equal(t, "active-acc", a)
}

func TestResolveTarget_UnsetReturnsEmptyAccountID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OANDA_ACCOUNT_ID", "")
	_, a, err := ResolveTarget("", false, "", false, "")
	require.NoError(t, err)
	assert.Equal(t, "", a)
}

func TestResolveTarget_UnknownBrokerErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, _, err := ResolveTarget("alpaca", true, "", false, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown broker")
}
