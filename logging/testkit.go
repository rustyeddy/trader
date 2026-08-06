package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Discard returns a *slog.Logger that discards everything logged through
// it. Use it in a component test that needs a valid logger but does not
// care what gets logged.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// Record is one structured log record captured by Capture, in a shape a
// test can assert on directly instead of parsing formatted console output.
// Attrs holds every attribute visible on the record, including ones added
// via Logger.With and WithGroup, with grouped attributes nested as
// map[string]any and any slog.LogValuer (including Secret) already
// resolved to its logged value — the same resolution a real handler
// performs.
type Record struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

// Recorder collects the records logged through the *slog.Logger that
// Capture returned it alongside. It is safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	records []Record
}

// Records returns every record captured so far, in the order they were
// logged. The returned records, including their Attrs maps, are an
// independent copy: mutating one has no effect on the Recorder's internal
// state or on any other slice Records has returned, including one returned
// by an earlier or later call.
func (r *Recorder) Records() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, len(r.records))
	for i, rec := range r.records {
		out[i] = rec
		out[i].Attrs = deepCopyAttrs(rec.Attrs)
	}
	return out
}

// deepCopyAttrs copies attrs, recursively copying any nested map[string]any
// produced by a grouped attribute (see addAttr). A shallow slice copy alone
// would still leave every Record's Attrs map — a reference type — aliased
// between the Recorder's internal state and whatever Records returns.
func deepCopyAttrs(attrs map[string]any) map[string]any {
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if nested, ok := v.(map[string]any); ok {
			v = deepCopyAttrs(nested)
		}
		out[k] = v
	}
	return out
}

// Reset discards every record captured so far, so one Recorder can be
// reused across the cases of a table-driven test instead of calling Capture
// again for each one.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = nil
}

func (r *Recorder) add(rec Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
}

// Capture returns a *slog.Logger and the *Recorder that captures everything
// logged through it — for a test that needs to assert on structured log
// output. The logger's handler is wrapped with NewContextHandler, so
// WithCorrelationID and WithCausationID work with it exactly as they do
// with a logger built by New.
func Capture() (*slog.Logger, *Recorder) {
	rec := &Recorder{}
	h := &recordingHandler{recorder: rec, level: slog.LevelDebug}
	return slog.New(NewContextHandler(h)), rec
}

// recordingHandler is a minimal slog.Handler that appends every record it
// receives to a Recorder instead of formatting it, resolving attribute
// values and honoring WithAttrs/WithGroup the same way a real handler does.
type recordingHandler struct {
	recorder *Recorder
	level    slog.Level
	attrs    []slog.Attr
	groups   []string
}

func (h *recordingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]any{}
	for _, a := range h.attrs {
		addAttr(attrs, h.groups, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		addAttr(attrs, h.groups, a)
		return true
	})

	h.recorder.add(Record{
		Time:    r.Time,
		Level:   r.Level,
		Message: r.Message,
		Attrs:   attrs,
	})
	return nil
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *recordingHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.groups = append(append([]string{}, h.groups...), name)
	return &next
}

// addAttr resolves a (running any slog.LogValuer, including Secret, to its
// logged value) and stores it in root at the path named by groups, creating
// nested maps for a slog.Group value the same way a real handler nests one.
func addAttr(root map[string]any, groups []string, a slog.Attr) {
	a.Value = a.Value.Resolve()

	if a.Value.Kind() == slog.KindGroup {
		nested := map[string]any{}
		for _, ga := range a.Value.Group() {
			addAttr(nested, nil, ga)
		}
		setNested(root, groups, a.Key, nested)
		return
	}
	setNested(root, groups, a.Key, a.Value.Any())
}

func setNested(root map[string]any, groups []string, key string, value any) {
	m := root
	for _, g := range groups {
		next, ok := m[g].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[g] = next
		}
		m = next
	}
	m[key] = value
}
