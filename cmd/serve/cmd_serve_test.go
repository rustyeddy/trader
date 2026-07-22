package serve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── loadConfig ────────────────────────────────────────────────────────────────

func TestLoadConfig_EmptyPathReturnsZeroValue(t *testing.T) {
	cfg, err := loadConfig("")
	require.NoError(t, err)
	assert.Equal(t, DaemonConfig{}, cfg)
}

func TestLoadConfig_WhitespacePathReturnsZeroValue(t *testing.T) {
	cfg, err := loadConfig("   ")
	require.NoError(t, err)
	assert.Equal(t, DaemonConfig{}, cfg)
}

func TestLoadConfig_ParsesAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trader.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
env: live
token: mytoken
account_id: ACC-123
rest:
  addr: ":8080"
journal:
  kind: csv
  tradespath: /var/trades.jsonl
  equitypath: /var/equity.jsonl
data:
  dir: /srv/data
log:
  level: info
  file: /var/log/trader.log
  format: json
`), 0o600))

	cfg, err := loadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "live", cfg.Env)
	assert.Equal(t, "mytoken", cfg.Token)
	assert.Equal(t, "ACC-123", cfg.AccountID)
	assert.Equal(t, ":8080", cfg.REST.Addr)
	assert.Equal(t, "csv", cfg.Journal.Kind)
	assert.Equal(t, "/var/trades.jsonl", cfg.Journal.TradesPath)
	assert.Equal(t, "/var/equity.jsonl", cfg.Journal.EquityPath)
	assert.Equal(t, "/srv/data", cfg.Data.Dir)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "/var/log/trader.log", cfg.Log.File)
	assert.Equal(t, "json", cfg.Log.Format)
}

func TestLoadConfig_MissingFileReturnsError(t *testing.T) {
	_, err := loadConfig("/nonexistent/path/trader.yaml")
	require.Error(t, err)
}

func TestLoadConfig_InvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`{not: valid: yaml: :`), 0o600))
	_, err := loadConfig(path)
	require.Error(t, err)
}

func TestLoadConfig_EmptyYAMLReturnsZeroValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))
	cfg, err := loadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, DaemonConfig{}, cfg)
}
