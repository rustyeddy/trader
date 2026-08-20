package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDataPlan_UsesRootLoggerForServiceRecords is issue #128's own
// end-to-end proof that buildDataContext hands the Service the exact
// logger root.go's own PersistentPreRunE already built from --log-*
// flags, rather than a second, independently configured one: this
// drives the real command tree (the same way vertical_slice_test.go's
// own TestM25VerticalSlice does), not a direct service/marketdata
// call, so a passing result proves the composition-root wiring is
// real, not merely that Service can log when handed a logger directly
// (service/marketdata's own log_test.go already covers that).
func TestDataPlan_UsesRootLoggerForServiceRecords(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "trader.log")

	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()

	root.SetArgs([]string{
		"--log-level", "DEBUG",
		"--log-format", "json",
		"--log-output", logPath,
		"data", "plan", "EURUSD", "H1",
		"--from", "2024-01-07T22:00:00Z", "--to", "2024-01-19T22:00:00Z",
		"--store-root", storeRoot,
		"--raw-root", rawRoot,
	})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NoError(t, cleanup())

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	var found bool
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "plan computed" {
			found = true
			assert.Equal(t, "marketdata", rec["component"])
			assert.Contains(t, rec, "instrument_id")
		}
	}
	assert.True(t, found, "expected a \"plan computed\" record in %s", logPath)
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
