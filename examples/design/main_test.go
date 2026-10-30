package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The margin check is still a stub, so every open is rejected. The demo must
// log that and finish rather than panic.
func TestRun_CompletesAndLogsMarginRejections(t *testing.T) {
	logs := captureLogs(t)

	require.NoError(t, run())

	out := logs.String()
	assert.Contains(t, out, "backtest starting")
	assert.Contains(t, out, "bars=521")
	assert.Contains(t, out, "intent rejected")
	assert.Contains(t, out, "instrument=EUR/USD")
	assert.Contains(t, out, "direction=long")
	assert.Contains(t, out, "time=20")
	assert.Contains(t, out, "backtest complete")
	assert.NotContains(t, out, "level=ERROR")
	assert.GreaterOrEqual(t, strings.Count(out, "intent rejected"), 1)
}
