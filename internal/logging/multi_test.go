package logging

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMulti_NoConfigsReturnsErrNoOutputs(t *testing.T) {
	_, _, err := NewMulti(nil)
	assert.ErrorIs(t, err, ErrNoOutputs)

	_, _, err = NewMulti([]Config{})
	assert.ErrorIs(t, err, ErrNoOutputs)
}

func TestNewMulti_SingleConfigBehavesLikeNew(t *testing.T) {
	multiPath := filepath.Join(t.TempDir(), "multi.log")
	newPath := filepath.Join(t.TempDir(), "new.log")
	cfgFor := func(path string) Config { return Config{Format: "json", Output: path} }

	mLogger, mCloser, err := NewMulti([]Config{cfgFor(multiPath)})
	require.NoError(t, err)
	mLogger.Info("hello", "n", 1)
	require.NoError(t, mCloser.Close())

	nLogger, nCloser, err := New(cfgFor(newPath))
	require.NoError(t, err)
	nLogger.Info("hello", "n", 1)
	require.NoError(t, nCloser.Close())

	multiData, err := os.ReadFile(multiPath)
	require.NoError(t, err)
	newData, err := os.ReadFile(newPath)
	require.NoError(t, err)

	// Both records carry a wall-clock "time" field, so compare every other
	// field rather than the raw bytes.
	var multiRec, newRec map[string]any
	require.NoError(t, json.Unmarshal(multiData, &multiRec))
	require.NoError(t, json.Unmarshal(newData, &newRec))
	delete(multiRec, "time")
	delete(newRec, "time")
	assert.Equal(t, newRec, multiRec)
}

func TestNewMulti_FansOutToEverySink(t *testing.T) {
	dir := t.TempDir()
	consolePath := filepath.Join(dir, "console.log")
	filePath := filepath.Join(dir, "trader.log")

	logger, closer, err := NewMulti([]Config{
		{Format: "text", Output: consolePath},
		{Format: "json", Output: filePath},
	})
	require.NoError(t, err)

	logger.Info("dataset published", "instrument", "EURUSD")
	require.NoError(t, closer.Close())

	consoleData, err := os.ReadFile(consolePath)
	require.NoError(t, err)
	assert.Contains(t, string(consoleData), "msg=\"dataset published\"")
	assert.Contains(t, string(consoleData), "instrument=EURUSD")

	fileData, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(fileData), `"msg":"dataset published"`)
	assert.Contains(t, string(fileData), `"instrument":"EURUSD"`)
}

// TestNewMulti_PerSinkLevelFiltering proves each sink keeps its own
// independent Level, per NewMulti's own doc comment ("sinks are not forced
// to share identical ... level filtering"): an INFO record reaches a
// DEBUG-level sink but not an ERROR-level one.
func TestNewMulti_PerSinkLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	verbosePath := filepath.Join(dir, "verbose.log")
	quietPath := filepath.Join(dir, "quiet.log")

	logger, closer, err := NewMulti([]Config{
		{Output: verbosePath},
		{Output: quietPath, Level: 8}, // slog.LevelError
	})
	require.NoError(t, err)

	logger.Info("routine event")
	require.NoError(t, closer.Close())

	verboseData, err := os.ReadFile(verbosePath)
	require.NoError(t, err)
	assert.Contains(t, string(verboseData), "routine event")

	quietData, err := os.ReadFile(quietPath)
	require.NoError(t, err)
	assert.Empty(t, quietData)
}

func TestNewMulti_ContextCorrelationPropagatesToEverySink(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.log")
	pathB := filepath.Join(dir, "b.log")

	logger, closer, err := NewMulti([]Config{{Output: pathA}, {Output: pathB}})
	require.NoError(t, err)

	ctx := WithCorrelationID(context.Background(), "corr-789")
	logger.InfoContext(ctx, "processing")
	require.NoError(t, closer.Close())

	for _, path := range []string{pathA, pathB} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(data), "correlation_id=corr-789")
	}
}

// TestNewMulti_PartialConstructionFailureClosesAlreadyOpenedSinks proves
// that when a later sink fails to build, an earlier, already-opened
// sink's file descriptor is genuinely released, not merely that
// NewMulti's code visits a Close call. It compares this process's open
// file-descriptor count (via /proc/self/fd, Linux/CI's own runner OS)
// immediately before and after the failing NewMulti call, rather than
// inferring closure indirectly.
func TestNewMulti_PartialConstructionFailureClosesAlreadyOpenedSinks(t *testing.T) {
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("requires /proc/self/fd (Linux)")
	}

	okPath := filepath.Join(t.TempDir(), "ok.log")
	before := countOpenFDs(t)

	_, _, err := NewMulti([]Config{
		{Output: okPath},
		{Format: "bogus-format"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")

	after := countOpenFDs(t)
	assert.Equal(t, before, after,
		"the first sink's file descriptor must be closed once the second sink fails to build")
}

func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	return len(entries)
}

// TestCloseAll_JoinsEveryCloseError uses a genuine double-Close on real
// *os.File values (the same technique issue #108's own
// TestNewRootCmd_CleanupRunsAfterFailingSubcommand uses): a second Close
// on an already-closed *os.File returns a real "file already closed"
// error, so a non-nil result here proves closeAll actually called Close
// on each entry, not merely that it compiles.
func TestCloseAll_JoinsEveryCloseError(t *testing.T) {
	dir := t.TempDir()
	f1, err := os.Create(filepath.Join(dir, "one.log"))
	require.NoError(t, err)
	f2, err := os.Create(filepath.Join(dir, "two.log"))
	require.NoError(t, err)

	require.NoError(t, f1.Close())
	require.NoError(t, f2.Close())

	err = closeAll([]io.Closer{f1, f2})
	require.Error(t, err)
	assert.Equal(t, 2, strings.Count(err.Error(), "file already closed"))
}

// TestMultiCloser_ClosePropagatesToEverySink is closeAll's own behavior,
// exercised through the exported surface (multiCloser.Close, as NewMulti's
// caller actually invokes it) rather than only in isolation.
func TestMultiCloser_ClosePropagatesToEverySink(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.log")
	pathB := filepath.Join(dir, "b.log")

	_, closer, err := NewMulti([]Config{{Output: pathA}, {Output: pathB}})
	require.NoError(t, err)
	require.NoError(t, closer.Close())

	// A second Close must not panic and should surface both underlying
	// "file already closed" errors, proving Close genuinely closed both
	// real files the first time rather than being a no-op.
	err = closer.Close()
	require.Error(t, err)
	assert.Equal(t, 2, strings.Count(err.Error(), "file already closed"))
}
