package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
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

// TestNestedWithCorrelationIDReplacesRatherThanDuplicates is the regression
// test for withAttr appending unconditionally: a second WithCorrelationID
// call on an already-scoped context used to leave both the old and new
// value in the stored attribute list, so every subsequent record carried
// two correlation_id keys. The later call — the more specific one for that
// point in the call chain — must replace the earlier value instead.
func TestNestedWithCorrelationIDReplacesRatherThanDuplicates(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "corr-outer")
	ctx = WithCorrelationID(ctx, "corr-inner")

	attrs := attrsFromContext(ctx)
	require.Len(t, attrs, 1, "the outer value must be replaced, not kept alongside the inner one")
	assert.Equal(t, "corr-inner", attrs[0].Value.String())

	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewTextHandler(&buf, nil)))
	logger.InfoContext(ctx, "processing")

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "correlation_id="),
		"exactly one correlation_id key must appear in the output")
	assert.Contains(t, out, "correlation_id=corr-inner")
	assert.NotContains(t, out, "corr-outer")
}

// TestNestedWithCausationIDReplacesRatherThanDuplicates is the same
// regression as above for CausationID, exercised independently since it is
// a distinct attribute key with its own call path through withAttr.
func TestNestedWithCausationIDReplacesRatherThanDuplicates(t *testing.T) {
	ctx := WithCausationID(context.Background(), "cause-outer")
	ctx = WithCausationID(ctx, "cause-inner")

	attrs := attrsFromContext(ctx)
	require.Len(t, attrs, 1)
	assert.Equal(t, "cause-inner", attrs[0].Value.String())
}

// TestExplicitRecordAttributeWinsOverContextValue is the regression test
// for contextHandler.Handle adding context attributes unconditionally: a
// record that already carries an explicit correlation_id attribute of its
// own used to end up with a second, context-injected correlation_id
// alongside it. The explicit, call-site value — more specific than the
// ambient context value — must win, and the record must carry only one
// correlation_id key.
func TestExplicitRecordAttributeWinsOverContextValue(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewTextHandler(&buf, nil)))

	ctx := WithCorrelationID(context.Background(), "corr-from-context")
	logger.InfoContext(ctx, "processing", CorrelationID, "corr-explicit")

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "correlation_id="),
		"exactly one correlation_id key must appear in the output")
	assert.Contains(t, out, "correlation_id=corr-explicit")
	assert.NotContains(t, out, "corr-from-context")
}

// TestExplicitRecordAttributeWinsOverContextValueInCapture confirms the
// same precedence through the Recorder-based test kit, where a duplicate
// key would show up as one map entry silently overwritten rather than as a
// visibly duplicated key — verifying the *value* that wins matters just as
// much as verifying there is only one.
func TestExplicitRecordAttributeWinsOverContextValueInCapture(t *testing.T) {
	logger, rec := Capture()

	ctx := WithCorrelationID(context.Background(), "corr-from-context")
	logger.InfoContext(ctx, "processing", CorrelationID, "corr-explicit")

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "corr-explicit", records[0].Attrs[CorrelationID])
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
