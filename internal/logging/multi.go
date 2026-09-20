package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
)

// ErrNoOutputs is returned by NewMulti when called with no Config values.
// Unlike New(Config{}), which has a sensible single-sink default (stderr,
// text, INFO), there is no sensible default that produces zero sinks.
var ErrNoOutputs = errors.New("logging: at least one output configuration is required")

// NewMulti builds one handler per Config in cfgs and fans every record out
// to all of them, returning a single *slog.Logger a caller uses exactly
// like one built by New. The primary use case (ADR-023, issue #127) is
// simultaneous operator console output plus persistent file logging:
//
//	logger, closer, err := logging.NewMulti([]logging.Config{
//	    {Format: "text", Output: "stderr"},
//	    {Format: "json", Output: "/var/log/trader/trader.log"},
//	})
//
// Each Config keeps its own independent Level, Format, and Output: sinks
// are not forced to share identical formatting or level filtering, so a
// console sink can stay human-readable at INFO while a file sink captures
// DEBUG as JSON, if a caller wants that. Passing Configs that differ only
// in Output — the common console+file case — makes every sink's
// formatting and level filtering consistent by construction, without
// NewMulti needing to impose that as a separate rule.
//
// NewMulti([]Config{cfg}) behaves the same as New(cfg); NewMulti holds no
// special case for exactly one sink beyond skipping the fan-out handler
// itself, so a caller choosing between the two APIs at runtime never needs
// its own special case for "just one output."
//
// The returned io.Closer closes every sink's own closer, in the order
// supplied, and joins every non-nil error with errors.Join rather than
// stopping at the first failure — a composition root always learns about
// every sink that failed to close cleanly. If any sink after the first
// fails to build (an unwritable file path, an unsupported Format), every
// previously-opened sink's closer is closed before NewMulti returns,
// joined onto the actual construction error, so a partial failure never
// leaks an open file descriptor and never silently swallows a cleanup
// error either.
func NewMulti(cfgs []Config) (*slog.Logger, io.Closer, error) {
	if len(cfgs) == 0 {
		return nil, nil, ErrNoOutputs
	}

	handlers := make([]slog.Handler, 0, len(cfgs))
	closers := make([]io.Closer, 0, len(cfgs))

	for _, cfg := range cfgs {
		w, closer, err := resolveOutput(cfg.Output)
		if err != nil {
			return nil, nil, errors.Join(err, closeAll(closers))
		}
		closers = append(closers, closer)

		h, err := newHandler(cfg, w)
		if err != nil {
			return nil, nil, errors.Join(err, closeAll(closers))
		}
		handlers = append(handlers, h)
	}

	if len(handlers) == 1 {
		return slog.New(NewContextHandler(handlers[0])), closers[0], nil
	}

	return slog.New(NewContextHandler(&multiHandler{handlers: handlers})), &multiCloser{closers: closers}, nil
}

// closeAll closes every non-nil closer in closers, joining every non-nil
// error with errors.Join rather than stopping at the first one.
func closeAll(closers []io.Closer) error {
	var errs []error
	for _, c := range closers {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// multiCloser closes every sink NewMulti opened. It is the returned
// io.Closer for any NewMulti call that builds more than one sink; the
// composition root that received it is the only owner of the resources it
// closes, the same "closer belongs to whoever called New/NewMulti" rule
// New's own doc comment already establishes.
type multiCloser struct {
	closers []io.Closer
}

func (c *multiCloser) Close() error {
	return closeAll(c.closers)
}

// multiHandler fans one record out to every wrapped handler, joining every
// non-nil Handle error with errors.Join rather than stopping at the first
// failure — one sink's write failure (a full disk, say) must not silently
// prevent the record from reaching a still-healthy sink.
type multiHandler struct {
	handlers []slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, sub := range h.handlers {
		if !sub.Enabled(ctx, r.Level) {
			continue
		}
		// r.Clone(), not r: slog.Record's own documentation requires a
		// handler that retains or re-iterates a Record to Clone it first,
		// since its Attrs are backed by a shared, possibly-reused
		// front-array; passing the same Record to more than one handler
		// without cloning risks each handler observing the others'
		// attribute mutations.
		if err := sub.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		next[i] = sub.WithAttrs(attrs)
	}
	return &multiHandler{handlers: next}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		next[i] = sub.WithGroup(name)
	}
	return &multiHandler{handlers: next}
}
