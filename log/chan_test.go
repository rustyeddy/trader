package log_test

import (
	"testing"
	"time"

	tlog "github.com/rustyeddy/trader/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drainChan reads up to n entries from ch within timeout, returning whatever
// was received.
func drainChan(ch <-chan tlog.LogEntry, n int, timeout time.Duration) []tlog.LogEntry {
	var out []tlog.LogEntry
	deadline := time.After(timeout)
	for i := 0; i < n; i++ {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-deadline:
			return out
		}
	}
	return out
}

// -------------------------------------------------------------------------
// Basic delivery
// -------------------------------------------------------------------------

func TestChan_Delivers_WhenEnabled(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: true}))

	ch := tlog.LogChan()
	require.NotNil(t, ch)

	tlog.Info("chan test message", "k", "v")

	entries := drainChan(ch, 1, time.Second)
	require.Len(t, entries, 1)
	assert.Equal(t, "chan test message", entries[0].Message)
}

func TestChan_Nil_WhenNotEnabled(t *testing.T) {
	// After a Setup with Chan: false the channel facility must not deliver
	// entries.  If the channel was never enabled, LogChan() may return nil
	// or a stale channel; either way no new entries should appear.
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: false}))

	ch := tlog.LogChan()
	// Drain any stale entries that may have been sent before this call.
	if ch != nil {
		for len(ch) > 0 {
			<-ch
		}
	}

	tlog.Info("should not reach channel")

	if ch != nil {
		entries := drainChan(ch, 1, 50*time.Millisecond)
		assert.Empty(t, entries, "no entries expected when Chan is disabled")
	}
}

func TestChan_NotDelivered_WhenChanDisabled(t *testing.T) {
	// Enable first so we have a valid channel reference.
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: true}))
	ch := tlog.LogChan()
	require.NotNil(t, ch)

	// Drain anything that may be lingering.
	for len(ch) > 0 {
		<-ch
	}

	// Re-setup without Chan; subsequent log calls must NOT reach the old channel.
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: false}))

	tlog.Info("should not be delivered")

	// Give a short time window; nothing should appear.
	entries := drainChan(ch, 1, 50*time.Millisecond)
	assert.Empty(t, entries, "no entries expected when Chan is disabled")
}

// -------------------------------------------------------------------------
// Level filtering
// -------------------------------------------------------------------------

func TestChan_LevelFiltering(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "warn", Chan: true}))
	ch := tlog.LogChan()

	tlog.Debug("debug msg")
	tlog.Info("info msg")
	tlog.Warn("warn msg")
	tlog.Error("error msg")

	entries := drainChan(ch, 4, 500*time.Millisecond)
	require.Len(t, entries, 2, "only warn and error should be delivered at warn level")
	assert.Equal(t, "warn msg", entries[0].Message)
	assert.Equal(t, "error msg", entries[1].Message)
}

// -------------------------------------------------------------------------
// Multiple entries
// -------------------------------------------------------------------------

func TestChan_MultipleEntries(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: true}))
	ch := tlog.LogChan()

	tlog.Debug("a")
	tlog.Info("b")
	tlog.Warn("c")

	entries := drainChan(ch, 3, time.Second)
	require.Len(t, entries, 3)
	assert.Equal(t, "a", entries[0].Message)
	assert.Equal(t, "b", entries[1].Message)
	assert.Equal(t, "c", entries[2].Message)
}

// -------------------------------------------------------------------------
// Attrs captured
// -------------------------------------------------------------------------

func TestChan_Attrs_Captured(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: true}))
	ch := tlog.LogChan()

	tlog.Info("with attrs", "instrument", "EURUSD", "price", 1.23)

	entries := drainChan(ch, 1, time.Second)
	require.Len(t, entries, 1)

	keys := make(map[string]bool)
	for _, a := range entries[0].Attrs {
		keys[a.Key] = true
	}
	assert.True(t, keys["instrument"], "expected 'instrument' attr")
	assert.True(t, keys["price"], "expected 'price' attr")
}

// -------------------------------------------------------------------------
// Fresh channel on re-setup
// -------------------------------------------------------------------------

func TestChan_FreshChannel_OnReSetup(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: true}))
	ch1 := tlog.LogChan()

	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: true}))
	ch2 := tlog.LogChan()

	// Each Setup with Chan: true must create a fresh channel.
	assert.NotEqual(t, ch1, ch2, "re-setup should produce a new channel")
}

// -------------------------------------------------------------------------
// Module loggers
// -------------------------------------------------------------------------

func TestChan_ModuleLogger_Delivered(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: true}))
	ch := tlog.LogChan()

	tlog.Data.Info("data event")
	tlog.Backtest.Warn("backtest warning")

	entries := drainChan(ch, 2, time.Second)
	require.Len(t, entries, 2)
	assert.Equal(t, "data event", entries[0].Message)
	assert.Equal(t, "backtest warning", entries[1].Message)
}

// -------------------------------------------------------------------------
// Chan and Memory can be enabled together
// -------------------------------------------------------------------------

func TestChan_AndMemory_Together(t *testing.T) {
	tlog.ClearEntries()
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", Chan: true, Memory: true}))
	ch := tlog.LogChan()

	tlog.Info("both facilities")

	chanEntries := drainChan(ch, 1, time.Second)
	memEntries := tlog.Entries()

	require.Len(t, chanEntries, 1)
	assert.Equal(t, "both facilities", chanEntries[0].Message)

	require.Len(t, memEntries, 1)
	assert.Equal(t, "both facilities", memEntries[0].Message)
}
