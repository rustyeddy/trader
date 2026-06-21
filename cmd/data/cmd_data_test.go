package data

import (
	"bytes"
	"testing"

	traderpkg "github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── New command structure ─────────────────────────────────────────────────────

func TestNew_HasExpectedSubcommands(t *testing.T) {
	cmd := New(nil)
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{
		"sync", "download-ticks", "build-candles",
		"oanda", "update", "candles", "stats",
		"pip-value", "position", "validate-candles",
	} {
		assert.True(t, names[want], "expected subcommand %q", want)
	}
}

// ── update --dry-run ──────────────────────────────────────────────────────────

func TestUpdateCmd_DryRun_PrintsInstrumentsAndTimeframes(t *testing.T) {
	cmd := newUpdateCmd(&traderpkg.RootConfig{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	_ = cmd.Flags().Set("instruments", "EURUSD,USDJPY")
	_ = cmd.Flags().Set("dry-run", "true")

	require.NoError(t, cmd.RunE(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "Dry run")
	assert.Contains(t, out, "EURUSD")
	assert.Contains(t, out, "USDJPY")
}

func TestUpdateCmd_DryRun_DefaultInstrumentsWhenNoneSpecified(t *testing.T) {
	cmd := newUpdateCmd(&traderpkg.RootConfig{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	_ = cmd.Flags().Set("dry-run", "true")

	require.NoError(t, cmd.RunE(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "Dry run")
	// defaultInstruments contains EUR_USD etc.; update uses them.
	assert.Contains(t, out, "Timeframes:")
}

func TestUpdateCmd_DryRun_WithFromDate(t *testing.T) {
	cmd := newUpdateCmd(&traderpkg.RootConfig{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	_ = cmd.Flags().Set("dry-run", "true")
	_ = cmd.Flags().Set("from", "2024-01-01")

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Contains(t, buf.String(), "Seed from: 2024-01-01")
}

func TestUpdateCmd_DryRun_BadFromReturnsError(t *testing.T) {
	cmd := newUpdateCmd(&traderpkg.RootConfig{})
	_ = cmd.Flags().Set("dry-run", "false")
	_ = cmd.Flags().Set("instruments", "EURUSD")
	_ = cmd.Flags().Set("from", "not-a-date")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--from")
}
