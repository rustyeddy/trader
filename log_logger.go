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
	"fmt"
	"io"
	"log/slog"
	"os"
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

	// File is an optional path to a log file.  When non-empty, log records
	// are written to both stdout and this file.
	File string

	// Syslog enables forwarding of log records to the system logger.
	// Has no effect on Windows (syslog is not available there).
	Syslog bool

	// Memory enables in-memory capture of log entries, accessible via
	// Entries() and ClearEntries().  Useful for testing and diagnostics.
	Memory bool
}

// -------------------------------------------------------------------------
// package state
// -------------------------------------------------------------------------

var (
	mu     sync.RWMutex
	level  = new(slog.LevelVar) // dynamic; adjusted by Setup
	defLog *slog.Logger

	modulesMu sync.RWMutex
	modules   = make(map[string]*slog.Logger)

	// Pre-wired module loggers.  They are initialised to the default logger
	// in init() and re-pointed whenever Setup is called.
	Data         *slog.Logger
	Backtest     *slog.Logger
	IndicatorLog *slog.Logger
	Strat        *slog.Logger
	Replay       *slog.Logger
)

func init() {
	level.Set(slog.LevelInfo)
	defLog = slog.Default()
	resetModules()
}

// resetModules rebuilds every pre-wired module logger from the current
// defLog.  Must be called with mu held for writing (or before concurrent use
// starts).
func resetModules() {
	modulesMu.Lock()
	defer modulesMu.Unlock()

	Data = defLog.With("module", "data")
	Backtest = defLog.With("module", "backtest")
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
// It is safe to call multiple times; subsequent calls replace the previous
// handler and re-wire all module loggers.
func Setup(cfg LogConfig) error {
	mu.Lock()
	defer mu.Unlock()

	// --- log level ---
	level.Set(parseLevel(cfg.Level))

	// --- build io.Writer ---
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	if cfg.File != "" {
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("log: open log file %q: %w", cfg.File, err)
		}
		writers = append(writers, f)
		// f is intentionally not closed here; the file stays open for the
		// lifetime of the process (subsequent Setup calls open a new handle).
	}

	if cfg.Syslog {
		sw, err := newSyslogWriter("trader")
		if err != nil {
			return fmt.Errorf("log: open syslog: %w", err)
		}
		writers = append(writers, sw)
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

	defLog = slog.New(h)
	slog.SetDefault(defLog)

	resetModules()
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

	mu.RLock()
	l := defLog.With("module", name)
	mu.RUnlock()

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
