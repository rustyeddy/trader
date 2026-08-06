package logging

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretRedactsUnderAnyKeyName(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logger.Info("event", "totally_unremarkable_key", Secret("super-secret-value"))

	out := buf.String()
	assert.Contains(t, out, "totally_unremarkable_key=REDACTED")
	assert.NotContains(t, out, "super-secret-value")
}

func TestSecretRedactsInJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	logger.Info("event", "field", Secret(12345))

	out := buf.String()
	assert.Contains(t, out, `"field":"REDACTED"`)
	assert.NotContains(t, out, "12345")
}

func TestNewRedactsCommonSensitiveKeyNames(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.log"
	logger, closer, err := New(Config{Output: path})
	require.NoError(t, err)

	logger.Info("login", "password", "hunter2", "username", "alice")
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "password=REDACTED")
	assert.NotContains(t, string(data), "hunter2")
	assert.Contains(t, string(data), "username=alice", "only sensitive-named keys are redacted")
}

func TestNewRedactsSensitiveKeysCaseInsensitively(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.log"
	logger, closer, err := New(Config{Output: path})
	require.NoError(t, err)

	logger.Info("login", "Password", "hunter2", "API_KEY", "sk-live-abc")
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "hunter2")
	assert.NotContains(t, string(data), "sk-live-abc")
}

func TestRedactSensitiveKeysLeavesGroupsAlone(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: redactSensitiveKeys})
	logger := slog.New(h)

	logger.Info("event", slog.Group("secret", "inner", "value"))

	out := buf.String()
	// The group itself is not collapsed to "REDACTED"; its leaf value,
	// whose key "inner" is not itself sensitive, passes through untouched.
	assert.Contains(t, out, `"secret":{"inner":"value"}`)
}
