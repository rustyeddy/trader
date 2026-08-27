package clictx

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggerFromContext_DefaultsWhenUnset(t *testing.T) {
	logger := LoggerFromContext(context.Background())
	require.NotNil(t, logger)
}

func TestWithLogger_RoundTrips(t *testing.T) {
	want := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := WithLogger(context.Background(), want)
	got := LoggerFromContext(ctx)
	assert.Same(t, want, got)
}

func TestEnvPrefix_IsTrader(t *testing.T) {
	assert.Equal(t, "TRADER", EnvPrefix)
}
