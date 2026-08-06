package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// Config configures a logger built by New. Its Go zero value is itself a
// valid, sensible configuration: Level's zero value is slog.LevelInfo,
// and both Format and Output resolve their empty string to their
// documented default, so logging.New(logging.Config{}) works.
type Config struct {
	// Level is the minimum record level that will be logged. Text form:
	// "DEBUG", "INFO", "WARN", or "ERROR" (see slog.Level.UnmarshalText).
	Level slog.Level `default:"INFO"`

	// Format selects the handler: "text" for human-readable output, "json"
	// for machine-readable output. Defaults to "text".
	Format string `default:"text" enum:"text,json"`

	// Output selects where records are written: "stderr" (the default),
	// "stdout", or any other value, which is treated as a file path to open
	// (creating it if necessary) and append to.
	Output string `default:"stderr"`
}

// New builds a *slog.Logger from cfg.
//
// The returned io.Closer must be closed by the caller when the logger is no
// longer needed — New starts no unmanaged background work, but a file
// output does hold an open file descriptor that the composition root owns
// the lifetime of. The closer is a no-op for "stderr" and "stdout".
func New(cfg Config) (*slog.Logger, io.Closer, error) {
	w, closer, err := resolveOutput(cfg.Output)
	if err != nil {
		return nil, nil, err
	}

	opts := &slog.HandlerOptions{Level: cfg.Level}

	var h slog.Handler
	switch cfg.Format {
	case "", "text":
		h = slog.NewTextHandler(w, opts)
	case "json":
		h = slog.NewJSONHandler(w, opts)
	default:
		_ = closer.Close()
		return nil, nil, fmt.Errorf("logging: unsupported format %q", cfg.Format)
	}

	return slog.New(h), closer, nil
}

// resolveOutput turns a Config.Output value into a writer and the closer
// that owns it. "" and "stderr" and "stdout" never need closing; anything
// else is opened as a file path, created if it does not already exist and
// appended to if it does, so restarting a long-running process does not
// truncate its own prior log output.
func resolveOutput(output string) (io.Writer, io.Closer, error) {
	switch output {
	case "", "stderr":
		return os.Stderr, nopCloser{}, nil
	case "stdout":
		return os.Stdout, nopCloser{}, nil
	default:
		f, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("logging: opening %s: %w", output, err)
		}
		return f, f, nil
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
