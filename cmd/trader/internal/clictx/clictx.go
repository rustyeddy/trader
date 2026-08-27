// Package clictx holds the small pieces of CLI composition-root state
// that both rootcmd (which builds the full command tree) and every
// command family package (cmd/trader/data, cmd/trader/broker, ...)
// need: the context-carried *slog.Logger PersistentPreRunE builds, and
// the TRADER_ environment-variable prefix every family's own
// config.Load call uses.
//
// This is a separate, lower-level package rather than living in
// rootcmd itself: rootcmd imports every command family to attach its
// commands (rootcmd -> data, rootcmd -> broker), so a family package
// cannot import rootcmd back without an import cycle. clictx has no
// dependency on rootcmd or any command family, so everyone can import
// it (issue #201).
package clictx

import (
	"context"
	"log/slog"
)

// EnvPrefix is the environment-variable prefix every trader flag's
// backing config.Load call uses, matching config's own documented
// convention for Trader's binaries (config/doc.go).
const EnvPrefix = "TRADER"

// loggerKey is an unexported context key type so this package's logger
// value can never collide with a key defined elsewhere.
type loggerKey struct{}

// WithLogger returns a copy of ctx carrying logger, retrievable by
// LoggerFromContext. Command handlers use this instead of a
// package-level global logger, keeping Trader's own determinism rule
// (no hidden global logger) intact even in the CLI's own bootstrap
// code.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFromContext returns the logger WithLogger placed on ctx, or
// slog.Default() if none was ever set — a command invoked outside the
// normal root PersistentPreRunE path (for example, directly in a test)
// still gets a usable, if unconfigured, logger rather than a nil
// dereference.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
