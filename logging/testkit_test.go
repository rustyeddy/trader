package logging

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscardDoesNotPanic(t *testing.T) {
	logger := Discard()
	require.NotNil(t, logger)
	assert.NotPanics(t, func() {
		logger.Info("anything", "key", "value")
		logger.Error("anything", "err", assert.AnError)
	})
}

func TestCaptureRecordsBasicFields(t *testing.T) {
	logger, rec := Capture()
	logger.Info("hello", "key", "value")

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelInfo, records[0].Level)
	assert.Equal(t, "hello", records[0].Message)
	assert.Equal(t, "value", records[0].Attrs["key"])
}

func TestCaptureRecordsMultipleInOrder(t *testing.T) {
	logger, rec := Capture()
	logger.Info("first")
	logger.Warn("second")
	logger.Error("third")

	records := rec.Records()
	require.Len(t, records, 3)
	assert.Equal(t, "first", records[0].Message)
	assert.Equal(t, "second", records[1].Message)
	assert.Equal(t, "third", records[2].Message)
}

func TestCaptureHonorsWithAttrs(t *testing.T) {
	logger, rec := Capture()
	scoped := logger.With("service", "trader")
	scoped.Info("started")

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "trader", records[0].Attrs["service"])
}

func TestCaptureHonorsWithGroup(t *testing.T) {
	logger, rec := Capture()
	scoped := logger.WithGroup("request")
	scoped.Info("handled", "status", 200)

	records := rec.Records()
	require.Len(t, records, 1)
	group, ok := records[0].Attrs["request"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, int64(200), group["status"])
}

func TestCaptureHonorsInlineGroup(t *testing.T) {
	logger, rec := Capture()
	logger.Info("event", slog.Group("http", "method", "GET", "status", 200))

	records := rec.Records()
	require.Len(t, records, 1)
	group, ok := records[0].Attrs["http"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "GET", group["method"])
	assert.Equal(t, int64(200), group["status"])
}

func TestCaptureResolvesSecret(t *testing.T) {
	logger, rec := Capture()
	logger.Info("authenticated", "password", Secret("hunter2"))

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "REDACTED", records[0].Attrs["password"])
}

func TestCaptureWorksWithComponent(t *testing.T) {
	logger, rec := Capture()
	scoped := WithComponent(logger, "broker")
	scoped.Info("connected")

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "broker", records[0].Attrs[Component])
}

func TestCapturePropagatesContext(t *testing.T) {
	logger, rec := Capture()
	ctx := WithCorrelationID(context.Background(), "corr-abc")
	logger.InfoContext(ctx, "processing")

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "corr-abc", records[0].Attrs[CorrelationID])
}

func TestRecorderReset(t *testing.T) {
	logger, rec := Capture()
	logger.Info("first")
	require.Len(t, rec.Records(), 1)

	rec.Reset()
	assert.Empty(t, rec.Records())

	logger.Info("second")
	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "second", records[0].Message)
}

func TestRecorderRecordsIsASnapshotCopy(t *testing.T) {
	logger, rec := Capture()
	logger.Info("first")

	snapshot := rec.Records()
	logger.Info("second")

	assert.Len(t, snapshot, 1, "a previously returned slice must not grow when more is logged")
	assert.Len(t, rec.Records(), 2)
}

// TestRecorderRecordsDeepCopiesAttrs is the regression test for Records
// returning a slice copy while every Record's Attrs map still aliased the
// Recorder's internal state: mutating a map obtained from one call to
// Records used to be visible in a later call, silently violating the
// "independent snapshot" contract Records documents.
func TestRecorderRecordsDeepCopiesAttrs(t *testing.T) {
	logger, rec := Capture()
	logger.Info("event", "key", "original")

	first := rec.Records()
	first[0].Attrs["key"] = "mutated"
	first[0].Attrs["injected"] = "should not leak"

	second := rec.Records()
	assert.Equal(t, "original", second[0].Attrs["key"],
		"mutating a returned record must not affect a later snapshot")
	_, present := second[0].Attrs["injected"]
	assert.False(t, present, "a key added to a returned map must not leak into a later snapshot")
}

// TestRecorderRecordsDeepCopiesNestedGroupAttrs covers the same guarantee
// for a grouped attribute's nested map, which deepCopyAttrs must recurse
// into rather than copying only the top level.
func TestRecorderRecordsDeepCopiesNestedGroupAttrs(t *testing.T) {
	logger, rec := Capture()
	logger.Info("event", slog.Group("http", "status", 200))

	first := rec.Records()
	group := first[0].Attrs["http"].(map[string]any)
	group["status"] = 999

	second := rec.Records()
	secondGroup := second[0].Attrs["http"].(map[string]any)
	assert.Equal(t, int64(200), secondGroup["status"],
		"mutating a nested group map must not affect a later snapshot")
}

func TestRecorderConcurrentUse(t *testing.T) {
	logger, rec := Capture()

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			logger.Info("concurrent")
		})
	}
	wg.Wait()

	assert.Len(t, rec.Records(), 50)
}

func TestCaptureRespectsDebugLevel(t *testing.T) {
	logger, rec := Capture()
	logger.Debug("visible because Capture defaults to Debug level")

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelDebug, records[0].Level)
}
