package logging

import (
	"context"
	"log/slog"
)

// Context-propagated correlation, built on slog's own context-aware
// methods (Logger.InfoContext, WarnContext, ErrorContext, DebugContext)
// rather than inventing new logging methods of our own. A *slog.Logger
// built by New already wraps its handler so this works automatically; a
// handler built some other way can opt in with NewContextHandler.

type ctxKey struct{}

// WithCorrelationID returns a context carrying id as this call chain's
// correlation ID. Every record logged through a context-aware method
// (logger.InfoContext(ctx, ...), and friends) on a logger whose handler
// passed through NewContextHandler — which New's handlers always do —
// automatically carries it as the CorrelationID attribute.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return withAttr(ctx, slog.String(CorrelationID, id))
}

// WithCausationID returns a context carrying id as this record's causation
// ID. See WithCorrelationID.
func WithCausationID(ctx context.Context, id string) context.Context {
	return withAttr(ctx, slog.String(CausationID, id))
}

func withAttr(ctx context.Context, attr slog.Attr) context.Context {
	existing, _ := ctx.Value(ctxKey{}).([]slog.Attr)

	// Copy rather than append in place: the existing slice's backing array
	// may be shared with a sibling context derived from the same parent
	// (e.g. two goroutines each adding their own causation ID to a shared
	// correlation-scoped context), and appending in place could let one
	// branch's attribute leak into the other.
	next := make([]slog.Attr, len(existing), len(existing)+1)
	copy(next, existing)
	next = append(next, attr)

	return context.WithValue(ctx, ctxKey{}, next)
}

func attrsFromContext(ctx context.Context) []slog.Attr {
	attrs, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	return attrs
}

// contextHandler decorates a slog.Handler, adding every attribute stored in
// the context (via WithCorrelationID, WithCausationID) to a record before
// delegating to the wrapped handler.
type contextHandler struct {
	slog.Handler
}

// NewContextHandler wraps h so that records logged through a context-aware
// slog.Logger method automatically carry whatever WithCorrelationID and
// WithCausationID stored on the context. New's handlers are already wrapped
// this way; call NewContextHandler directly only when building a
// *slog.Logger some other way (a custom handler chain, a test harness) that
// still wants context propagation.
func NewContextHandler(h slog.Handler) slog.Handler {
	return &contextHandler{Handler: h}
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs := attrsFromContext(ctx); len(attrs) > 0 {
		r.AddAttrs(attrs...)
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}
