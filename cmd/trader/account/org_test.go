package account

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrg_Golden(t *testing.T) {
	e := newEnv(t).synced(t)
	for name, args := range map[string][]string{
		"trades.org.golden":           {"trades", "--format", "org"},
		"trades_benchmark.org.golden": {"trades", "--format", "org", "--benchmark", "broker-vwap"},
	} {
		out, err := e.run(t, args...)
		require.NoError(t, err, name)
		golden(t, name, out)
	}
	out, err := e.run(t, "trades", "show", firstTradeID(t, e), "--format", "org", "--benchmark", "broker-quote")
	require.NoError(t, err)
	golden(t, "show.org.golden", out)

	r := newEnvWith(t, reversalFixture()).synced(t)
	out, err = r.run(t, "trades", "--format", "org", "--benchmark", "broker-vwap")
	require.NoError(t, err)
	golden(t, "trades_reversal.org.golden", out)
}

func TestOrg_IsDeterministicAndHeadedByOpenTime(t *testing.T) {
	e := newEnv(t).synced(t)
	a, err := e.run(t, "trades", "--format", "org")
	require.NoError(t, err)
	b, err := e.run(t, "trades", "--format", "org")
	require.NoError(t, err)
	assert.Equal(t, a, b)

	js, err := e.run(t, "trades", "--format", "json")
	require.NoError(t, err)
	for _, tr := range decode(t, js)["trades"].([]any) {
		m := tr.(map[string]any)
		opened, closed := m["opened_at"].(string), m["closed_at"].(string)
		require.NotEqual(t, opened, closed)
		assert.Contains(t, a, "* "+opened+" ", "the heading is the open time")
		assert.NotContains(t, a, "* "+closed+" ")
	}
}

func TestOrg_UnknownIsNotZero(t *testing.T) {
	out, err := newEnv(t).synced(t).run(t, "trades", "--format", "org")
	require.NoError(t, err)
	// Trade 2->3 never had spread or other fees reported.
	assert.Contains(t, out, ":SPREAD_COST: unknown\n")
	assert.Contains(t, out, ":OTHER_FEES: unknown\n")
	assert.Contains(t, out, ":NET: unknown\n")
	// Trade 4->5 has known zeros.
	assert.Contains(t, out, ":OTHER_FEES: 0.00 USD (derived)\n")
	assert.NotContains(t, out, "SLIPPAGE", "no benchmark was requested")
}

func TestOrg_RendersCompletedTradesOnly(t *testing.T) {
	e := newEnv(t).synced(t)
	cases := map[string][]string{
		"list":            {"activity", "list", "--format", "org"},
		"open":            {"trades", "--open", "--format", "org"},
		"show open trade": {"trades", "show", "oanda/practice/acct/6#long", "--format", "org"},
	}
	for name, args := range cases {
		out, err := e.run(t, args...)
		require.Error(t, err, name)
		assert.ErrorContains(t, err, "completed trades only", name)
		assert.NotContains(t, out, "* ", name)
	}
}

// sync must refuse the format before it fetches or stores anything, not run
// and then fail to print what it did.
func TestOrg_SyncIsRefusedBeforeAnyWork(t *testing.T) {
	e := newEnv(t)
	_, err := e.run(t, "activity", "sync", "--format", "org")
	require.ErrorContains(t, err, "completed trades only")
	assert.Zero(t, e.src.calls, "the provider was called")
	out, err := e.run(t, "activity", "list", "--format", "json")
	require.NoError(t, err)
	assert.Empty(t, decode(t, out)["activities"], "nothing was stored")
}

func TestOrg_EmptyRangeSaysSo(t *testing.T) {
	out, err := newEnv(t).synced(t).run(t, "trades", "--format", "org", "--from", "2030-01-01")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(out, "# no completed trades in range\n"), out)
	assert.NotContains(t, out, "\n* ")
}

func TestFormatHelpAndErrorNameOrg(t *testing.T) {
	_, err := newEnv(t).run(t, "trades", "--format", "xml")
	assert.ErrorContains(t, err, "table, json or org")
}
