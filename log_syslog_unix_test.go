//go:build !windows

package trader

import (
	"errors"
	"log/slog"
	"log/syslog"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeSyslogBackend struct {
	lastMethod string
	lastMsg    string
	err        error
}

func (f *fakeSyslogBackend) Debug(msg string) error {
	f.lastMethod = "debug"
	f.lastMsg = msg
	return f.err
}

func (f *fakeSyslogBackend) Info(msg string) error {
	f.lastMethod = "info"
	f.lastMsg = msg
	return f.err
}

func (f *fakeSyslogBackend) Warning(msg string) error {
	f.lastMethod = "warning"
	f.lastMsg = msg
	return f.err
}

func (f *fakeSyslogBackend) Err(msg string) error {
	f.lastMethod = "err"
	f.lastMsg = msg
	return f.err
}

func TestLevelToPriorityTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		msg   string
		want  syslog.Priority
	}{
		{name: "text debug", msg: "time=2026-01-01T00:00:00Z level=" + slog.LevelDebug.String() + " msg=test", want: syslog.LOG_DEBUG},
		{name: "text warn", msg: "time=2026-01-01T00:00:00Z level=" + slog.LevelWarn.String() + " msg=test", want: syslog.LOG_WARNING},
		{name: "text error", msg: "time=2026-01-01T00:00:00Z level=" + slog.LevelError.String() + " msg=test", want: syslog.LOG_ERR},
		{name: "json debug", msg: `{"time":"2026-01-01T00:00:00Z","level":"` + slog.LevelDebug.String() + `","msg":"test"}`, want: syslog.LOG_DEBUG},
		{name: "json warn", msg: `{"time":"2026-01-01T00:00:00Z","level":"` + slog.LevelWarn.String() + `","msg":"test"}`, want: syslog.LOG_WARNING},
		{name: "json error", msg: `{"time":"2026-01-01T00:00:00Z","level":"` + slog.LevelError.String() + `","msg":"test"}`, want: syslog.LOG_ERR},
		{name: "default info", msg: "time=2026-01-01T00:00:00Z msg=test", want: syslog.LOG_INFO},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, levelToPriority(tc.msg))
		})
	}
}

func TestSyslogWriterWriteMappingAndTrim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantMethod string
	}{
		{name: "debug", input: "time=2026-01-01T00:00:00Z level=DEBUG msg=test\n", wantMethod: "debug"},
		{name: "warn", input: "time=2026-01-01T00:00:00Z level=WARN msg=test\n", wantMethod: "warning"},
		{name: "error", input: "time=2026-01-01T00:00:00Z level=ERROR msg=test\n", wantMethod: "err"},
		{name: "default", input: "time=2026-01-01T00:00:00Z msg=test\n", wantMethod: "info"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeSyslogBackend{}
			sw := &syslogWriter{w: backend}

			n, err := sw.Write([]byte(tc.input))
			assert.NoError(t, err)
			assert.Equal(t, len(tc.input), n)
			assert.Equal(t, tc.wantMethod, backend.lastMethod)
			assert.NotContains(t, backend.lastMsg, "\n")
		})
	}
}

func TestSyslogWriterWritePropagatesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("backend failed")
	backend := &fakeSyslogBackend{err: wantErr}
	sw := &syslogWriter{w: backend}
	input := "time=2026-01-01T00:00:00Z level=ERROR msg=test\n"

	n, err := sw.Write([]byte(input))
	assert.Equal(t, len(input), n)
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, "err", backend.lastMethod)
}
