package trader

// This file implements the in-memory log stack: a bounded, thread-safe slice
// of captured log entries that can be read or cleared at any time.  The stack
// is enabled by setting LogConfig.Memory = true in a Setup call.
//
// Typical usage:
//
//	Setup(LogConfig{Level: "debug", Memory: true})
//	Info("something happened", "key", "value")
//
//	entries := Entries()  // inspect captured records
//	ClearEntries()        // reset the stack

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// -------------------------------------------------------------------------
// LogEntry
// -------------------------------------------------------------------------

// LogEntry is a single structured log record stored in the in-memory stack.
type LogEntry struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
}

// -------------------------------------------------------------------------
// package-level stack state
// -------------------------------------------------------------------------

var (
	stackMu  sync.RWMutex
	logStack []LogEntry
)

// Entries returns a snapshot (copy) of all log entries currently held in the
// in-memory stack.  It is safe to call from multiple goroutines.
func Entries() []LogEntry {
	stackMu.RLock()
	defer stackMu.RUnlock()
	if len(logStack) == 0 {
		return nil
	}
	cp := make([]LogEntry, len(logStack))
	copy(cp, logStack)
	return cp
}

// ClearEntries discards all entries held in the in-memory stack.
func ClearEntries() {
	stackMu.Lock()
	logStack = logStack[:0]
	stackMu.Unlock()
}

// -------------------------------------------------------------------------
// stackHandler – slog.Handler that appends to logStack
// -------------------------------------------------------------------------

// stackHandler implements slog.Handler and appends every handled record to
// the package-level logStack slice.
type stackHandler struct {
	attrs  []slog.Attr
	groups []string
}

func (h *stackHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= level.Level()
}

func (h *stackHandler) Handle(_ context.Context, r slog.Record) error {
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

	stackMu.Lock()
	logStack = append(logStack, e)
	stackMu.Unlock()
	return nil
}

func (h *stackHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(combined, h.attrs)
	copy(combined[len(h.attrs):], attrs)
	return &stackHandler{attrs: combined, groups: h.groups}
}

func (h *stackHandler) WithGroup(name string) slog.Handler {
	groups := make([]string, len(h.groups)+1)
	copy(groups, h.groups)
	groups[len(groups)-1] = name
	return &stackHandler{attrs: h.attrs, groups: groups}
}

// -------------------------------------------------------------------------
// multiHandler – fans out to multiple slog.Handler instances
// -------------------------------------------------------------------------

// multiHandler implements slog.Handler by forwarding every call to each of
// the contained handlers in order.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: hs}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: hs}
}
