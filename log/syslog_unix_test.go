//go:build !windows

package log

import (
	"context"
	"errors"
	"log/slog"
	"log/syslog"
	"testing"
	"time"

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

func (f *fakeSyslogBackend) Close() error {
	return f.err
}

func TestLevelToPriorityTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lvl  slog.Level
		want syslog.Priority
	}{
		{name: "debug", lvl: slog.LevelDebug, want: syslog.LOG_DEBUG},
		{name: "warn", lvl: slog.LevelWarn, want: syslog.LOG_WARNING},
		{name: "error", lvl: slog.LevelError, want: syslog.LOG_ERR},
		{name: "info", lvl: slog.LevelInfo, want: syslog.LOG_INFO},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, levelToPriority(tc.lvl))
		})
	}
}

func TestSyslogHandlerMappingAndFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		level      slog.Level
		wantMethod string
	}{
		{name: "debug", level: slog.LevelDebug, wantMethod: "debug"},
		{name: "warn", level: slog.LevelWarn, wantMethod: "warning"},
		{name: "error", level: slog.LevelError, wantMethod: "err"},
		{name: "default", level: slog.LevelInfo, wantMethod: "info"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeSyslogBackend{}
			h := &syslogHandler{w: backend, attrs: []slog.Attr{slog.String("module", "test")}}
			r := slog.NewRecord(testTime(t), tc.level, "test", 0)
			r.AddAttrs(slog.String("instrument", "EURUSD"))

			err := h.Handle(context.Background(), r)
			assert.NoError(t, err)
			assert.Equal(t, tc.wantMethod, backend.lastMethod)
			assert.Contains(t, backend.lastMsg, "test")
			assert.Contains(t, backend.lastMsg, "module=test")
			assert.Contains(t, backend.lastMsg, "instrument=EURUSD")
		})
	}
}

func TestSyslogHandlerPropagatesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("backend failed")
	backend := &fakeSyslogBackend{err: wantErr}
	h := &syslogHandler{w: backend}
	r := slog.NewRecord(testTime(t), slog.LevelError, "test", 0)

	err := h.Handle(context.Background(), r)
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, "err", backend.lastMethod)
}

func TestNewSyslogHandler_SuccessAndError(t *testing.T) {
	t.Parallel()

	orig := syslogNew
	t.Cleanup(func() { syslogNew = orig })

	t.Run("success", func(t *testing.T) {
		backend := &fakeSyslogBackend{}
		syslogNew = func(p syslog.Priority, tag string) (syslogBackend, error) {
			assert.Equal(t, syslog.LOG_INFO|syslog.LOG_USER, p)
			assert.Equal(t, "trader-test", tag)
			return backend, nil
		}

		sw, err := newSyslogHandler("trader-test")
		assert.NoError(t, err)
		assert.NotNil(t, sw)
		assert.Same(t, backend, sw.w)
	})

	t.Run("error", func(t *testing.T) {
		wantErr := errors.New("dial failed")
		syslogNew = func(p syslog.Priority, tag string) (syslogBackend, error) {
			return nil, wantErr
		}

		sw, err := newSyslogHandler("trader-test")
		assert.Nil(t, sw)
		assert.Error(t, err)
		assert.ErrorIs(t, err, wantErr)
		assert.Contains(t, err.Error(), "syslog.New")
	})
}

func TestSyslogHandlerClose(t *testing.T) {
	t.Parallel()

	backend := &fakeSyslogBackend{}
	sw := &syslogHandler{w: backend}
	assert.NoError(t, sw.Close())

	wantErr := errors.New("close failed")
	swErr := &syslogHandler{w: &fakeSyslogBackend{err: wantErr}}
	assert.ErrorIs(t, swErr.Close(), wantErr)
}

func testTime(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}
