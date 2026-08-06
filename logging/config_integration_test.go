package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigLoadsLoggingConfig is the concrete proof for the package doc
// comment's claim that logging.Config works with config.Load with no
// special-casing: slog.Level already implements encoding.TextUnmarshaler.
func TestConfigLoadsLoggingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")

	cfg, err := config.Load[Config](config.Options{
		Environ:     []string{},
		FileContent: []byte("level: WARN\nformat: json\noutput: " + path + "\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, slog.LevelWarn, cfg.Level)
	assert.Equal(t, "json", cfg.Format)

	logger, closer, err := New(cfg)
	require.NoError(t, err)
	logger.Warn("hello")
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"msg":"hello"`)
}
