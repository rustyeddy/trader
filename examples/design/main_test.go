package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sim account has margin, so the demo runs to completion without any
// rejections or errors.
func TestRun_CompletesWithoutRejections(t *testing.T) {
	logs := captureLogs(t)

	require.NoError(t, run())

	out := logs.String()
	assert.Contains(t, out, "backtest starting")
	assert.Contains(t, out, "bars=521")
	assert.Contains(t, out, "backtest complete")
	assert.NotContains(t, out, "order rejected")
	assert.NotContains(t, out, "level=ERROR")
}
