// Package trader provides structured logging for the trader application using
// Go's standard log/slog library.  It supports multiple concurrent output
// destinations (stdout, a log file, and syslog) and named module loggers so
// that log records can be filtered by subsystem (data, backtest, indicator,
// replay, …).
//
// Typical usage:
//
//	// initialise once at startup (e.g. from main or cmd layer)
//	Setup(LogConfig{Level: "debug", Format: "text", File: "trader.log"})
//
//	// package-level helpers
//	Info("server started", "port", 8080)
//	Debug("tick received", "instrument", "EURUSD")
//
//	// module-scoped logger
//	logger := Module("data")
//	logger.Info("inventory built", "files", 42)
//
//	// or use the pre-wired module variables
//	Data.Info("download complete", "key", key)
//	Backtest.Warn("end of data reached")
package trader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// -------------------------------------------------------------------------
// LogConfig
// -------------------------------------------------------------------------

// LogConfig holds the logging configuration that is typically populated from
// the application's RootConfig (RootConfig.LogLevel, etc.).
type LogConfig struct {
	// Level is the minimum log level to emit.  Accepted values (case-
	// insensitive): "debug", "info", "warn" / "warning", "error".
	// Defaults to "info" when empty or unrecognised.
	Level string

	// Format selects the handler format: "json" for JSON output, anything
	// else (or empty) for human-readable text.
	Format string

	// File is an optional path to a log file. When non-empty, log records
	// are written to both stdout and this file. When empty and no other sink
	// is configured, Setup falls back to a default log file.
	File string

	// Syslog enables forwarding of log records to the system logger.
	// Has no effect on Windows (syslog is not available there).
	Syslog bool

	// Stdout enables log output to stdout
	Stdout bool

	// Memory enables in-memory capture of log entries, accessible via
	// Entries() and ClearEntries().  Useful for testing and diagnostics.
	Memory bool
}

// -------------------------------------------------------------------------
// package state
// -------------------------------------------------------------------------

var (
	mu sync.Mutex

	level        = new(slog.LevelVar) // dynamic; adjusted by Setup
	handlerState *switchHandlerState
	defLog       *slog.Logger
	sinkClosers  []io.Closer

	modulesMu sync.RWMutex
	modules   = make(map[string]*slog.Logger)

	// Pre-wired module loggers.  They are initialised to the default logger
	// in init() and remain valid across Setup calls.
	L            *slog.Logger
	Data         *slog.Logger
	BacktestLog  *slog.Logger
	IndicatorLog *slog.Logger
	Strat        *slog.Logger
	Replay       *slog.Logger
)

const defaultLogFile = "trader.log"

func init() {
	level.Set(slog.LevelInfo)

	// Keep logger pointers stable for the lifetime of the process.
	base := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: level})
	handlerState = newSwitchHandlerState(base)
	defLog = slog.New(&switchHandler{state: handlerState})
	slog.SetDefault(defLog)

	resetModules()
}

// resetModules rebuilds every pre-wired module logger from defLog.
func resetModules() {
	modulesMu.Lock()
	defer modulesMu.Unlock()

	L = defLog.With("module", "trader")
	Data = defLog.With("module", "data")
	BacktestLog = defLog.With("module", "backtest")
	IndicatorLog = defLog.With("module", "indicator")
	Replay = defLog.With("module", "replay")
	Strat = defLog.With("module", "strategies")

	// Refresh any previously requested ad-hoc module loggers.
	for name := range modules {
		modules[name] = defLog.With("module", name)
	}
}

// -------------------------------------------------------------------------
// Setup
// -------------------------------------------------------------------------

// Setup initialises (or re-initialises) the logging system according to cfg.
// It is safe to call multiple times; subsequent calls replace the active
// handler and close previously opened sinks.
func Setup(cfg LogConfig) error {
	mu.Lock()
	defer mu.Unlock()

	// --- log level ---
	level.Set(parseLevel(cfg.Level))

	if cfg.File == "" && !cfg.Stdout && !cfg.Syslog {
		cfg.File = defaultLogFile
	}

	if err := closeSinksLocked(); err != nil {
		return err
	}

	// --- build io.Writer + closers ---
	var writers []io.Writer
	closers := make([]io.Closer, 0, 2)
	if cfg.Stdout {
		writers = append(writers, os.Stdout)
	}

	if cfg.File != "" {
		if dir := filepath.Dir(cfg.File); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("log: create log directory %q: %w", dir, err)
			}
		}
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("log: open log file %q: %w", cfg.File, err)
		}
		writers = append(writers, f)
		closers = append(closers, f)
	}

	if cfg.Syslog {
		sw, err := newSyslogWriter("trader")
		if err != nil {
			return fmt.Errorf("log: open syslog: %w", err)
		}
		writers = append(writers, sw)
		if c, ok := any(sw).(io.Closer); ok {
			closers = append(closers, c)
		}
	}
	if len(writers) == 0 {
		writers = append(writers, io.Discard)
	}

	w := io.MultiWriter(writers...)

	// --- handler ---
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if strings.ToLower(cfg.Format) == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}

	if cfg.Memory {
		h = &multiHandler{handlers: []slog.Handler{h, &stackHandler{}}}
	}

	handlerState.Set(h)
	sinkClosers = closers
	return nil
}

// -------------------------------------------------------------------------
// Module
// -------------------------------------------------------------------------

// Module returns a *slog.Logger pre-populated with the attribute
// "module"=name.  The same logger is returned on subsequent calls with the
// same name.
func Module(name string) *slog.Logger {
	modulesMu.RLock()
	if l, ok := modules[name]; ok {
		modulesMu.RUnlock()
		return l
	}
	modulesMu.RUnlock()

	modulesMu.Lock()
	defer modulesMu.Unlock()
	l := defLog.With("module", name)

	modules[name] = l
	return l
}

// -------------------------------------------------------------------------
// Package-level helpers (delegate to the default logger)
// -------------------------------------------------------------------------

// Debug logs at LevelDebug.
func Debug(msg string, args ...any) { defLog.Debug(msg, args...) }

// Info logs at LevelInfo.
func Info(msg string, args ...any) { defLog.Info(msg, args...) }

// Warn logs at LevelWarn.
func Warn(msg string, args ...any) { defLog.Warn(msg, args...) }

// Error logs at LevelError.
func Error(msg string, args ...any) { defLog.Error(msg, args...) }

// Fatal logs at LevelError and terminates the process with os.Exit(1).
func Fatal(msg string, args ...any) {
	defLog.Error(msg, args...)
	os.Exit(1)
}

// -------------------------------------------------------------------------
// helpers
// -------------------------------------------------------------------------

func closeSinksLocked() error {
	var firstErr error
	for _, c := range sinkClosers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("log: close previous sink: %w", err)
		}
	}
	sinkClosers = nil
	return firstErr
}

type switchHandlerState struct {
	mu sync.RWMutex
	h  slog.Handler
}

func newSwitchHandlerState(h slog.Handler) *switchHandlerState {
	return &switchHandlerState{h: h}
}

func (s *switchHandlerState) Set(h slog.Handler) {
	s.mu.Lock()
	s.h = h
	s.mu.Unlock()
}

func (s *switchHandlerState) get() slog.Handler {
	s.mu.RLock()
	h := s.h
	s.mu.RUnlock()
	return h
}

type switchHandler struct {
	state *switchHandlerState
	ops   []handlerOp
}

func (h *switchHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.state.get().Enabled(ctx, l)
}

func (h *switchHandler) Handle(ctx context.Context, r slog.Record) error {
	active := applyHandlerOps(h.state.get(), h.ops)
	return active.Handle(ctx, r)
}

func (h *switchHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	ops := make([]handlerOp, len(h.ops), len(h.ops)+1)
	copy(ops, h.ops)
	attrsCopy := make([]slog.Attr, len(attrs))
	copy(attrsCopy, attrs)
	ops = append(ops, handlerOp{kind: handlerOpWithAttrs, attrs: attrsCopy})
	return &switchHandler{state: h.state, ops: ops}
}

func (h *switchHandler) WithGroup(name string) slog.Handler {
	ops := make([]handlerOp, len(h.ops), len(h.ops)+1)
	copy(ops, h.ops)
	ops = append(ops, handlerOp{kind: handlerOpWithGroup, group: name})
	return &switchHandler{state: h.state, ops: ops}
}

const (
	handlerOpWithAttrs = iota
	handlerOpWithGroup
)

type handlerOp struct {
	kind  int
	attrs []slog.Attr
	group string
}

func applyHandlerOps(base slog.Handler, ops []handlerOp) slog.Handler {
	h := base
	for _, op := range ops {
		switch op.kind {
		case handlerOpWithAttrs:
			h = h.WithAttrs(op.attrs)
		case handlerOpWithGroup:
			h = h.WithGroup(op.group)
		}
	}
	return h
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
