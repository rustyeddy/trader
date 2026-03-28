//go:build !windows

package log

import (
	"fmt"
	"log/slog"
	"log/syslog"
	"strings"
)

// syslogWriter wraps a *syslog.Writer so that it implements io.Writer and
// maps slog severity levels to syslog priorities.  When used as a plain
// io.Writer (e.g. inside a slog.TextHandler / slog.JSONHandler), every write
// is forwarded as syslog.LOG_INFO.
type syslogWriter struct {
	w *syslog.Writer
}

// newSyslogWriter creates a syslogWriter connected to the local syslog
// daemon under the given program tag.
func newSyslogWriter(tag string) (*syslogWriter, error) {
	w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_USER, tag)
	if err != nil {
		return nil, fmt.Errorf("syslog.New: %w", err)
	}
	return &syslogWriter{w: w}, nil
}

// Write implements io.Writer.  It strips a trailing newline (syslog adds its
// own) and derives the priority from the slog level field in the record.
// Both text format ("… level=DEBUG …") and JSON format ("level":"DEBUG") are
// recognised.  The level field is matched with a leading space or quote so
// that message content containing the same substring is not misidentified.
func (sw *syslogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	n := len(p)

	pri := levelToPriority(msg)
	switch pri {
	case syslog.LOG_DEBUG:
		return n, sw.w.Debug(msg)
	case syslog.LOG_WARNING:
		return n, sw.w.Warning(msg)
	case syslog.LOG_ERR:
		return n, sw.w.Err(msg)
	default:
		return n, sw.w.Info(msg)
	}
}

// levelToPriority maps the slog level field embedded in a formatted log
// record to a syslog priority.  It handles both slog text format
// (" level=DEBUG ") and JSON format (`"level":"DEBUG"`).
func levelToPriority(s string) syslog.Priority {
	// Text format: fields are space-separated; level appears as " level=NAME"
	// (always preceded by a space because "time=…" comes first).
	textPrefixes := []struct {
		pat string
		pri syslog.Priority
	}{
		{" level=" + slog.LevelDebug.String(), syslog.LOG_DEBUG},
		{" level=" + slog.LevelWarn.String(), syslog.LOG_WARNING},
		{" level=" + slog.LevelError.String(), syslog.LOG_ERR},
	}
	for _, p := range textPrefixes {
		if strings.Contains(s, p.pat) {
			return p.pri
		}
	}

	// JSON format: `"level":"DEBUG"` etc.
	jsonPairs := []struct {
		pat string
		pri syslog.Priority
	}{
		{`"level":"` + slog.LevelDebug.String() + `"`, syslog.LOG_DEBUG},
		{`"level":"` + slog.LevelWarn.String() + `"`, syslog.LOG_WARNING},
		{`"level":"` + slog.LevelError.String() + `"`, syslog.LOG_ERR},
	}
	for _, p := range jsonPairs {
		if strings.Contains(s, p.pat) {
			return p.pri
		}
	}

	return syslog.LOG_INFO
}
