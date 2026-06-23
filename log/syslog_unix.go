//go:build !windows

package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"log/syslog"
	"strings"
)

type syslogHandler struct {
	w      syslogBackend
	attrs  []slog.Attr
	groups []string
}

type syslogBackend interface {
	Debug(string) error
	Info(string) error
	Warning(string) error
	Err(string) error
	Close() error
}

var syslogNew = func(p syslog.Priority, tag string) (syslogBackend, error) {
	return syslog.New(p, tag)
}

// newSyslogHandler creates a syslogHandler connected to the local syslog
// daemon under the given program tag.
func newSyslogHandler(tag string) (*syslogHandler, error) {
	w, err := syslogNew(syslog.LOG_INFO|syslog.LOG_USER, tag)
	if err != nil {
		return nil, fmt.Errorf("syslog.New: %w", err)
	}
	return &syslogHandler{w: w}, nil
}

func (h *syslogHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= level.Level()
}

func (h *syslogHandler) Handle(_ context.Context, r slog.Record) error {
	msg := formatSyslogMessage(r, h.attrs, h.groups)
	switch levelToPriority(r.Level) {
	case syslog.LOG_DEBUG:
		return h.w.Debug(msg)
	case syslog.LOG_WARNING:
		return h.w.Warning(msg)
	case syslog.LOG_ERR:
		return h.w.Err(msg)
	default:
		return h.w.Info(msg)
	}
}

func (h *syslogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	qualified := attrs
	if len(h.groups) > 0 {
		qualified = make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			qualified[i] = qualifyWithGroups(a, h.groups)
		}
	}
	combined := make([]slog.Attr, len(h.attrs)+len(qualified))
	copy(combined, h.attrs)
	copy(combined[len(h.attrs):], qualified)
	return &syslogHandler{w: h.w, attrs: combined, groups: h.groups}
}

func (h *syslogHandler) WithGroup(name string) slog.Handler {
	groups := make([]string, len(h.groups)+1)
	copy(groups, h.groups)
	groups[len(groups)-1] = name
	return &syslogHandler{w: h.w, attrs: h.attrs, groups: groups}
}

func (h *syslogHandler) Close() error {
	return h.w.Close()
}

func formatSyslogMessage(r slog.Record, baseAttrs []slog.Attr, groups []string) string {
	var b strings.Builder
	b.WriteString(r.Message)

	writeAttr := func(a slog.Attr) {
		a.Value = a.Value.Resolve()
		if a.Equal(slog.Attr{}) || a.Key == "" {
			return
		}
		b.WriteString(" ")
		b.WriteString(a.Key)
		b.WriteString("=")
		b.WriteString(attrValueString(a.Value))
	}

	for _, a := range baseAttrs {
		writeAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(qualifyWithGroups(a, groups))
		return true
	})
	return b.String()
}

func attrValueString(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format("2006-01-02T15:04:05Z07:00")
	default:
		return fmt.Sprint(v.Any())
	}
}

func levelToPriority(l slog.Level) syslog.Priority {
	switch {
	case l <= slog.LevelDebug:
		return syslog.LOG_DEBUG
	case l >= slog.LevelError:
		return syslog.LOG_ERR
	case l >= slog.LevelWarn:
		return syslog.LOG_WARNING
	default:
		return syslog.LOG_INFO
	}
}

var _ io.Closer = (*syslogHandler)(nil)
var _ io.Closer = (*syslogHandler)(nil)
