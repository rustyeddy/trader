package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
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

func TestCanonicalComponentNames(t *testing.T) {
	// Same reasoning as TestCanonicalAttributeNames: these are a public
	// contract every subsystem's own composition-root wiring agrees on by
	// name, so a rename must be a visible, deliberate diff.
	assert.Equal(t, "marketdata", ComponentMarketData)
	assert.Equal(t, "broker", ComponentBroker)
	assert.Equal(t, "account", ComponentAccount)
	assert.Equal(t, "orders", ComponentOrders)
	assert.Equal(t, "portfolio", ComponentPortfolio)
	assert.Equal(t, "strategy", ComponentStrategy)
	assert.Equal(t, "backtest", ComponentBacktest)
	assert.Equal(t, "execution", ComponentExecution)
	assert.Equal(t, "service", ComponentService)
	assert.Equal(t, "cli", ComponentCLI)
}

// TestWithComponent_TextAndJSONOutput is issue #126's own end-to-end
// demonstration that a component-scoped logger's Component attribute
// actually survives into both supported output formats, built via New the
// same way a real composition root would (not just via a bare
// slog.NewTextHandler, which TestWithComponent above already covers) —
// text output is line-oriented ("component=marketdata"), JSON output is a
// structured field ("component":"marketdata").
func TestWithComponent_TextAndJSONOutput(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.log")
			logger, closer, err := New(Config{Format: format, Output: path})
			require.NoError(t, err)

			scoped := WithComponent(logger, ComponentMarketData)
			scoped.Info("dataset published")
			require.NoError(t, closer.Close())

			data, err := os.ReadFile(path)
			require.NoError(t, err)

			switch format {
			case "text":
				assert.Contains(t, string(data), "component=marketdata")
			case "json":
				assert.Contains(t, string(data), `"component":"marketdata"`)
			}
		})
	}
}
