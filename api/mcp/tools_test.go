package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSrv() *Server {
	return New(slog.Default(), "")
}

func TestListAccountsToolRegistered(t *testing.T) {
	srv := newTestSrv()
	names := map[string]bool{}
	for _, td := range srv.tools() {
		names[td.Name] = true
	}
	assert.True(t, names["list_accounts"], "list_accounts tool must be advertised")
}

func TestAccountSummaryAndOrdersToolsRegistered(t *testing.T) {
	srv := newTestSrv()
	names := map[string]bool{}
	for _, td := range srv.tools() {
		names[td.Name] = true
	}
	assert.True(t, names["account_summary"], "account_summary tool must be advertised")
	assert.True(t, names["account_orders"], "account_orders tool must be advertised")
}

// account_summary/account_orders build their own broker from
// OANDA_TOKEN/~/.config/oanda/pat.txt (same as list_accounts), so only the
// deterministic auth-failure path is testable without a real token.
func TestToolAccountSummary_MissingToken(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	srv := newTestSrv()
	got, rpcErr := srv.toolAccountSummary(context.Background(), nil)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), "no token")
}

func TestToolAccountOrders_MissingToken(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	srv := newTestSrv()
	got, rpcErr := srv.toolAccountOrders(context.Background(), nil)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), "no token")
}

// ── infra tools ───────────────────────────────────────────────────────────

func TestToolGetVersion(t *testing.T) {
	srv := newTestSrv()
	got, rpcErr := srv.toolGetVersion()
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"version"`)
}

func TestToolGetHealth(t *testing.T) {
	srv := newTestSrv()
	got, rpcErr := srv.toolGetHealth()
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"status": "ok"`)
}

func TestGetVersionAllowedWithoutOANDA(t *testing.T) {
	srv := newTestSrv()
	raw, _ := json.Marshal(map[string]any{"name": "get_version", "arguments": map[string]any{}})
	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	assert.NotEqual(t, true, payload["isError"])
}

func TestHandleToolsCall_UnknownTool(t *testing.T) {
	srv := newTestSrv()
	raw, _ := json.Marshal(map[string]any{"name": "bogus_tool", "arguments": map[string]any{}})
	_, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.NotNil(t, rpcErr)
	assert.Contains(t, rpcErr.Message, "unknown tool")
}
