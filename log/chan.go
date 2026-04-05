package log

// This file implements the channel-based logging facility.  When
// Config.Chan is true, every log record that passes the level filter is
// forwarded (non-blocking) to a buffered channel that callers can receive
// from via LogChan().
//
// Typical usage:
//
//	tlog.Setup(tlog.Config{Level: "debug", Chan: true})
//
//	go func() {
//	    for entry := range tlog.LogChan() {
//	        fmt.Println(entry.Level, entry.Message)
//	    }
//	}()
//
//	tlog.Info("something happened", "key", "value")

import (
	"context"
	"log/slog"
	"sync"
)

// chanBufSize is the capacity of the log-entry channel.  A large buffer
// keeps the logging path non-blocking under burst conditions.
const chanBufSize = 256

// -------------------------------------------------------------------------
// package-level channel state
// -------------------------------------------------------------------------

var (
	chanMu  sync.RWMutex
	logChan chan LogEntry
)

// LogChan returns the read-only channel that receives log entries when
// Config.Chan is enabled.  The channel is (re-)created on each Setup call
// that enables Chan; it is nil when the facility has never been enabled.
//
// Callers should range over the channel in a separate goroutine.  The
// channel is buffered (capacity chanBufSize); entries are dropped silently
// when the buffer is full to keep the logging path non-blocking.
func LogChan() <-chan LogEntry {
	chanMu.RLock()
	defer chanMu.RUnlock()
	return logChan
}

// newLogChan creates a fresh buffered channel, stores it as the package-level
// logChan, and returns it.  It is safe to call concurrently.
func newLogChan() chan LogEntry {
	chanMu.Lock()
	defer chanMu.Unlock()
	logChan = make(chan LogEntry, chanBufSize)
	return logChan
}

// -------------------------------------------------------------------------
// chanHandler – slog.Handler that publishes to logChan
// -------------------------------------------------------------------------

// chanHandler implements slog.Handler.  It sends each record to the
// package-level logChan using a non-blocking select so that a slow consumer
// never blocks the caller.
//
// groups tracks the active group chain so that sub-handlers created by
// WithGroup carry the correct context.  Groups are not expanded into the
// flat LogEntry.Attrs slice; callers who need group-prefixed keys should
// inspect the slog.Record directly.
type chanHandler struct {
	ch     chan LogEntry
	attrs  []slog.Attr
	groups []string
}

func (h *chanHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= level.Level()
}

func (h *chanHandler) Handle(_ context.Context, r slog.Record) error {
	e := LogEntry{
		Time:    r.Time,
		Level:   r.Level,
		Message: r.Message,
		Attrs:   make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs()),
	}
	e.Attrs = append(e.Attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		e.Attrs = append(e.Attrs, a)
		return true
	})

	select {
	case h.ch <- e:
	default:
		// Channel full; drop the entry rather than blocking the caller.
	}
	return nil
}

func (h *chanHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(combined, h.attrs)
	copy(combined[len(h.attrs):], attrs)
	return &chanHandler{ch: h.ch, attrs: combined, groups: h.groups}
}

func (h *chanHandler) WithGroup(name string) slog.Handler {
	groups := make([]string, len(h.groups)+1)
	copy(groups, h.groups)
	groups[len(h.groups)] = name
	return &chanHandler{ch: h.ch, attrs: h.attrs, groups: groups}
}
