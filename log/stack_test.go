package log_test

import (
	"testing"

	tlog "github.com/rustyeddy/trader/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -------------------------------------------------------------------------
// Entries / ClearEntries
// -------------------------------------------------------------------------

func TestStack_Captures_WhenMemoryEnabled(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: true}))

	tlog.Info("stack message", "key", "value")

	entries := tlog.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "stack message", entries[0].Message)
}

func TestStack_NotCaptured_WhenMemoryDisabled(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: false}))

	tlog.Info("no stack")

	assert.Empty(t, tlog.Entries())
}

func TestStack_LevelFiltering(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "warn", Memory: true}))

	tlog.Debug("debug msg")
	tlog.Info("info msg")
	tlog.Warn("warn msg")
	tlog.Error("error msg")

	entries := tlog.Entries()
	require.Len(t, entries, 2, "only warn and error should be captured at warn level")
	assert.Equal(t, "warn msg", entries[0].Message)
	assert.Equal(t, "error msg", entries[1].Message)
}

func TestStack_MultipleEntries(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: true}))

	tlog.Debug("a")
	tlog.Info("b")
	tlog.Warn("c")

	entries := tlog.Entries()
	require.Len(t, entries, 3)
	assert.Equal(t, "a", entries[0].Message)
	assert.Equal(t, "b", entries[1].Message)
	assert.Equal(t, "c", entries[2].Message)
}

func TestStack_ClearEntries(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: true}))

	tlog.Info("before clear")
	require.Len(t, tlog.Entries(), 1)

	tlog.ClearEntries()
	assert.Empty(t, tlog.Entries())
}

func TestStack_Entries_ReturnsCopy(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: true}))

	tlog.Info("original")
	snap := tlog.Entries()

	// Adding another entry after taking the snapshot must not affect snap.
	tlog.Info("new entry")
	assert.Len(t, snap, 1, "snapshot should not reflect entries added after the call")
}

func TestStack_Attrs_Captured(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: true}))

	tlog.Info("with attrs", "instrument", "EURUSD", "price", 1.23)

	entries := tlog.Entries()
	require.Len(t, entries, 1)

	// Attrs should contain instrument and price keys.
	keys := make(map[string]bool)
	for _, a := range entries[0].Attrs {
		keys[a.Key] = true
	}
	assert.True(t, keys["instrument"], "expected 'instrument' attr")
	assert.True(t, keys["price"], "expected 'price' attr")
}

func TestStack_ResetAfterSetup_WithoutMemory(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: true}))
	tlog.Info("in memory")
	require.Len(t, tlog.Entries(), 1)

	// Re-setup without Memory; stack should receive no new entries.
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: false}))
	tlog.ClearEntries()
	tlog.Info("not in memory")
	assert.Empty(t, tlog.Entries())
}

// -------------------------------------------------------------------------
// Module loggers capture to stack
// -------------------------------------------------------------------------

func TestStack_ModuleLogger_Captured(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: true}))

	tlog.Data.Info("data event")
	tlog.Backtest.Warn("backtest warning")

	entries := tlog.Entries()
	require.Len(t, entries, 2)
	assert.Equal(t, "data event", entries[0].Message)
	assert.Equal(t, "backtest warning", entries[1].Message)
}

func TestStack_Entries_EmptyBeforeAnyLog(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Memory: true}))
	assert.Empty(t, tlog.Entries())
}
