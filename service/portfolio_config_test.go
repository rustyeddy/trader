package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── LoadPortfolioConfig ──────────────────────────────────────────────────────

func TestLoadPortfolioConfig_ParsesNonDefaultValues(t *testing.T) {
	content := `
env: live
risk_pct: 2.5
drawdown_circuit_pct: 15.0
local_warmup_bars: 200
instruments:
  - instrument: USD_CHF
    timeframe: H1
    strategy:
      kind: noop
`
	f := filepath.Join(t.TempDir(), "p.yml")
	require.NoError(t, os.WriteFile(f, []byte(content), 0o644))

	cfg, err := LoadPortfolioConfig(f)
	require.NoError(t, err)
	assert.Equal(t, "live", cfg.Env)
	assert.Equal(t, 2.5, cfg.RiskPct)
	assert.Equal(t, 15.0, cfg.DrawdownCircuitPct)
	assert.Equal(t, 200, cfg.LocalWarmupBars)
	require.Len(t, cfg.Instruments, 1)
	assert.Equal(t, "USD_CHF", cfg.Instruments[0].Instrument)
	assert.Equal(t, "H1", cfg.Instruments[0].Timeframe)
}

func TestLoadPortfolioConfig_InvalidYAML(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.yml")
	require.NoError(t, os.WriteFile(f, []byte(":\tinvalid: yaml: :::"), 0o644))

	_, err := LoadPortfolioConfig(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse portfolio config")
}

func TestLoadPortfolioConfig_Defaults(t *testing.T) {
	// Write a minimal config to a temp file.
	content := []byte(`instruments: []`)
	f := t.TempDir() + "/p.yml"
	require.NoError(t, os.WriteFile(f, content, 0o644))

	cfg, err := LoadPortfolioConfig(f)
	require.NoError(t, err)
	assert.Equal(t, "practice", cfg.Env)
	assert.Equal(t, 1.0, cfg.RiskPct)
	assert.Equal(t, 10.0, cfg.DrawdownCircuitPct)
}

func TestLoadPortfolioConfig_MissingFile(t *testing.T) {
	_, err := LoadPortfolioConfig("/nonexistent/path.yml")
	require.Error(t, err)
}
