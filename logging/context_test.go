package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextHandlerAddsCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewTextHandler(&buf, nil)))

	ctx := WithCorrelationID(context.Background(), "corr-123")
	logger.InfoContext(ctx, "processing")

	assert.Contains(t, buf.String(), "correlation_id=corr-123")
}

func TestContextHandlerAddsCausationID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewTextHandler(&buf, nil)))

	ctx := WithCausationID(context.Background(), "cause-456")
	logger.InfoContext(ctx, "processing")

	assert.Contains(t, buf.String(), "causation_id=cause-456")
}

func TestContextHandlerCombinesBoth(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewTextHandler(&buf, nil)))

	ctx := WithCorrelationID(context.Background(), "corr-123")
	ctx = WithCausationID(ctx, "cause-456")
	logger.InfoContext(ctx, "processing")

	out := buf.String()
	assert.Contains(t, out, "correlation_id=corr-123")
	assert.Contains(t, out, "causation_id=cause-456")
}

func TestContextHandlerWithoutContextAttrsIsUnaffected(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewTextHandler(&buf, nil)))

	logger.InfoContext(context.Background(), "processing")

	out := buf.String()
	assert.NotContains(t, out, "correlation_id")
	assert.NotContains(t, out, "causation_id")
}

func TestContextHandlerPlainInfoStillWorks(t *testing.T) {
	// Handle must work correctly even when the caller uses the
	// non-context-aware logging methods (Info, Warn, ...): slog derives a
	// context.Background() internally for those, so this must not panic and
	// must simply omit any context-only attributes.
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewTextHandler(&buf, nil)))

	logger.Info("processing")

	assert.Contains(t, buf.String(), "msg=processing")
}

// TestSiblingContextsDoNotLeakAttributes guards the copy-on-write design in
// withAttr: two contexts derived from one shared parent must not influence
// each other's attributes, which an in-place append to a shared backing
// array could allow.
func TestSiblingContextsDoNotLeakAttributes(t *testing.T) {
	parent := WithCorrelationID(context.Background(), "corr-shared")

	child1 := WithCausationID(parent, "cause-1")
	child2 := WithCausationID(parent, "cause-2")

	attrs1 := attrsFromContext(child1)
	attrs2 := attrsFromContext(child2)

	require.Len(t, attrs1, 2)
	require.Len(t, attrs2, 2)
	assert.Equal(t, "cause-1", attrs1[1].Value.String())
	assert.Equal(t, "cause-2", attrs2[1].Value.String())
}

func TestContextHandlerWithAttrsPreservesWrapping(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewTextHandler(&buf, nil)))

	scoped := logger.With("static", "value")
	ctx := WithCorrelationID(context.Background(), "corr-789")
	scoped.InfoContext(ctx, "processing")

	out := buf.String()
	assert.Contains(t, out, "static=value")
	assert.Contains(t, out, "correlation_id=corr-789")
}

// TestContextHandlerWithGroupPreservesWrapping checks that WithGroup keeps
// working through contextHandler's decoration. Once a group is open,
// everything logged through that handler — including attributes injected
// from the context — nests inside it; that is ordinary slog group
// semantics; a context-injected attribute "escaping" an open group would be
// the surprising behavior.
func TestContextHandlerWithGroupPreservesWrapping(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))

	scoped := logger.WithGroup("req")
	ctx := WithCorrelationID(context.Background(), "corr-789")
	scoped.InfoContext(ctx, "processing", "field", 1)

	out := buf.String()
	assert.Contains(t, out, `"req":{"field":1,"correlation_id":"corr-789"}`)
}

func TestNewLoggerPropagatesContextAutomatically(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.log"
	logger, closer, err := New(Config{Output: path})
	require.NoError(t, err)
	defer closer.Close()

	ctx := WithCorrelationID(context.Background(), "corr-from-new")
	logger.InfoContext(ctx, "processing")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "correlation_id=corr-from-new")
}
