package logging

import (
	"bytes"
	"fmt"
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

// TestSecretWrapperCannotLeakThroughOrdinaryFormatting is the regression
// test for secretValue previously retaining the wrapped value in a field.
// A LogValuer's job is to control what an slog handler prints, but nothing
// stops other code from formatting or reflecting over the wrapper directly
// -- fmt.Sprintf("%v", ...) does not know about slog.LogValuer and will
// happily print an unexported field's content. The wrapper must therefore
// hold nothing worth printing, not just resolve to "REDACTED" through the
// interface slog itself calls.
func TestSecretWrapperCannotLeakThroughOrdinaryFormatting(t *testing.T) {
	const original = "hunter2-super-secret"

	wrapped := Secret(original)

	assert.NotContains(t, fmt.Sprintf("%v", wrapped), original)
	assert.NotContains(t, fmt.Sprintf("%+v", wrapped), original)
	assert.NotContains(t, fmt.Sprintf("%#v", wrapped), original)

	// The wrapper itself must carry no fields at all, not merely fields that
	// happen not to print the secret.
	assert.Equal(t, secretValue{}, wrapped)
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
