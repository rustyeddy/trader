package main

import (
	"context"
	"log/slog"
)

// loggerKey is an unexported context key type so this package's logger
// value can never collide with a key defined elsewhere.
type loggerKey struct{}

// withLogger returns a copy of ctx carrying logger, retrievable by
// loggerFromContext. Command handlers (#109-#112) use this instead of
// a package-level global logger, keeping trader's own determinism rule
// (no hidden global logger) intact even in the CLI's own bootstrap
// code.
func withLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// loggerFromContext returns the logger withLogger placed on ctx, or
// slog.Default() if none was ever set — a command invoked outside the
// normal root PersistentPreRunE path (for example, directly in a test)
// still gets a usable, if unconfigured, logger rather than a nil
// dereference.
func loggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
