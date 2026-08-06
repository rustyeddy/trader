package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewZeroValueConfigIsUsable(t *testing.T) {
	logger, closer, err := New(Config{})
	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.NoError(t, closer.Close())
}

func TestNewTextFormat(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, nil)
	logger := slog.New(h)
	logger.Info("hello", "key", "value")

	assert.Contains(t, buf.String(), "msg=hello")
	assert.Contains(t, buf.String(), "key=value")
}

func TestNewSelectsTextHandler(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")

	logger, closer, err := New(Config{Format: "text", Output: path})
	require.NoError(t, err)
	logger.Info("hello world")
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "msg=\"hello world\"")
	assert.NotContains(t, string(data), "{")
}

func TestNewSelectsJSONHandler(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")

	logger, closer, err := New(Config{Format: "json", Output: path})
	require.NoError(t, err)
	logger.Info("hello world")
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"msg":"hello world"`)
}

func TestNewRejectsUnsupportedFormat(t *testing.T) {
	_, _, err := New(Config{Format: "xml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xml")
}

func TestNewRespectsLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")

	logger, closer, err := New(Config{Output: path, Level: slog.LevelWarn})
	require.NoError(t, err)
	logger.Info("should be filtered out")
	logger.Warn("should appear")
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "should be filtered out")
	assert.Contains(t, string(data), "should appear")
}

func TestNewDefaultLevelIsInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")

	logger, closer, err := New(Config{Output: path})
	require.NoError(t, err)
	logger.Debug("should be filtered out")
	logger.Info("should appear")
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "should be filtered out")
	assert.Contains(t, string(data), "should appear")
}

func TestResolveOutputStderrAndStdout(t *testing.T) {
	w, closer, err := resolveOutput("stderr")
	require.NoError(t, err)
	assert.Same(t, os.Stderr, w)
	assert.NoError(t, closer.Close())

	w, closer, err = resolveOutput("stdout")
	require.NoError(t, err)
	assert.Same(t, os.Stdout, w)
	assert.NoError(t, closer.Close())

	w, closer, err = resolveOutput("")
	require.NoError(t, err)
	assert.Same(t, os.Stderr, w, "empty output defaults to stderr")
	assert.NoError(t, closer.Close())
}

func TestResolveOutputArbitraryFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "any", "where.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	w, closer, err := resolveOutput(path)
	require.NoError(t, err)
	_, err = w.Write([]byte("line\n"))
	require.NoError(t, err)
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "line\n", string(data))
}

func TestResolveOutputAppendsRatherThanTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")
	require.NoError(t, os.WriteFile(path, []byte("existing\n"), 0o644))

	w, closer, err := resolveOutput(path)
	require.NoError(t, err)
	_, err = w.Write([]byte("new\n"))
	require.NoError(t, err)
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "existing\nnew\n", string(data))
}

func TestResolveOutputInvalidPathErrors(t *testing.T) {
	_, _, err := resolveOutput(string([]byte{0}))
	require.Error(t, err)
}

func TestNewRejectsUnsupportedFormatClosesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")

	_, _, err := New(Config{Format: "xml", Output: path})
	require.Error(t, err)

	// The file must have been closed despite the error, not leaked open.
	require.NoError(t, os.Remove(path))
}
