package mcp

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerEffectiveReportsDir_Default(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	assert.Equal(t, defaultReportsDir, srv.effectiveReportsDir())
}

func TestServerWithReportsDir_OverridesDefault(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	srv.WithReportsDir("/tmp/custom-reports")
	assert.Equal(t, "/tmp/custom-reports", srv.effectiveReportsDir())
}

func TestReadBacktestResource_ListUsesConfiguredReportsDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run-a.org"), []byte("* run a\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run-b.org"), []byte("* run b\n"), 0o644))

	srv := New(&service.Service{Log: slog.Default()}, false)
	srv.WithReportsDir(dir)

	got, rpcErr := srv.readBacktestResource("backtest://results")
	require.Nil(t, rpcErr)

	payload := got.(map[string]any)
	contents := payload["contents"].([]map[string]any)
	require.Len(t, contents, 1)

	text := contents[0]["text"].(string)
	assert.Contains(t, text, "run-a.org")
	assert.Contains(t, text, "run-b.org")
}

func TestReadBacktestResource_ReadSpecificOrgFromConfiguredReportsDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run-a.org"), []byte("* run a\n"), 0o644))

	srv := New(&service.Service{Log: slog.Default()}, false)
	srv.WithReportsDir(dir)

	got, rpcErr := srv.readBacktestResource("backtest://results/run-a")
	require.Nil(t, rpcErr)

	payload := got.(map[string]any)
	contents := payload["contents"].([]map[string]any)
	require.Len(t, contents, 1)
	assert.Equal(t, "* run a\n", contents[0]["text"])
}

func TestHandleResourcesRead_BacktestResultsUsesConfiguredReportsDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run-a.org"), []byte("* run a\n"), 0o644))

	srv := New(&service.Service{Log: slog.Default()}, false)
	srv.WithReportsDir(dir)

	raw, err := json.Marshal(map[string]any{"uri": "backtest://results"})
	require.NoError(t, err)

	got, rpcErr := srv.handleResourcesRead(t.Context(), raw)
	require.Nil(t, rpcErr)

	payload := got.(map[string]any)
	contents := payload["contents"].([]map[string]any)
	require.Len(t, contents, 1)
	assert.Contains(t, contents[0]["text"], "run-a.org")
}
