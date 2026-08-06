package logging

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalAttributeNames(t *testing.T) {
	// These are a public contract: components across the codebase agree on
	// them by name. Pin the literal strings so a rename is a visible,
	// deliberate diff, not an accidental one.
	assert.Equal(t, "run_id", RunID)
	assert.Equal(t, "session_id", SessionID)
	assert.Equal(t, "account_id", AccountID)
	assert.Equal(t, "instrument_id", InstrumentID)
	assert.Equal(t, "order_id", OrderID)
	assert.Equal(t, "correlation_id", CorrelationID)
	assert.Equal(t, "causation_id", CausationID)
	assert.Equal(t, "component", Component)
}

func TestWithComponent(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, nil))

	scoped := WithComponent(base, "broker")
	scoped.Info("connected")

	require.Contains(t, buf.String(), "component=broker")
	assert.Contains(t, buf.String(), "msg=connected")
}

func TestWithComponentDoesNotAffectBaseLogger(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, nil))

	_ = WithComponent(base, "broker")
	base.Info("unscoped")

	assert.NotContains(t, buf.String(), "component=")
}
