package service

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Register "noop" backtest strategy and "pulse" live strategy for build tests.
	_ "github.com/rustyeddy/trader/strategies/noop"
	_ "github.com/rustyeddy/trader/strategies/pulse"
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

// ── BuildPortfolioRunConfig ──────────────────────────────────────────────────

func testLogger() *slog.Logger { return slog.Default() }

func TestBuildPortfolioRunConfig_EmptyInstruments(t *testing.T) {
	cfg := &PortfolioConfig{
		DrawdownCircuitPct: 5.0,
		RiskPct:            1.0,
	}
	rc, err := BuildPortfolioRunConfig(cfg, nil, "", testLogger())
	require.NoError(t, err)
	assert.Empty(t, rc.Instruments)
	assert.Equal(t, 5.0, rc.DrawdownCircuitPct)
}

func TestBuildPortfolioRunConfig_BacktestStrategy(t *testing.T) {
	cfg := &PortfolioConfig{
		RiskPct: 1.0,
		Instruments: []portfolioInstrumentYAML{
			{
				Instrument: "EUR_USD",
				Timeframe:  "H1",
				Strategy:   struct {
					Kind   string         `yaml:"kind"`
					Params map[string]any `yaml:"params"`
				}{Kind: "noop"},
			},
		},
	}
	rc, err := BuildPortfolioRunConfig(cfg, nil, "", testLogger())
	require.NoError(t, err)
	require.Len(t, rc.Instruments, 1)
	assert.Equal(t, "EUR_USD", rc.Instruments[0].Instrument)
	assert.Equal(t, 1.0, rc.Instruments[0].RiskPct)
}

func TestBuildPortfolioRunConfig_RiskPctFallsBackToPortfolioDefault(t *testing.T) {
	cfg := &PortfolioConfig{
		RiskPct: 2.0,
		Instruments: []portfolioInstrumentYAML{
			{
				Instrument: "EUR_USD",
				Timeframe:  "H1",
				RiskPct:    0, // no per-instrument override
				Strategy:   struct {
					Kind   string         `yaml:"kind"`
					Params map[string]any `yaml:"params"`
				}{Kind: "noop"},
			},
		},
	}
	rc, err := BuildPortfolioRunConfig(cfg, nil, "", testLogger())
	require.NoError(t, err)
	require.Len(t, rc.Instruments, 1)
	assert.Equal(t, 2.0, rc.Instruments[0].RiskPct)
}

func TestBuildPortfolioRunConfig_PerInstrumentRiskPctOverridesDefault(t *testing.T) {
	cfg := &PortfolioConfig{
		RiskPct: 1.0,
		Instruments: []portfolioInstrumentYAML{
			{
				Instrument: "EUR_USD",
				Timeframe:  "H1",
				RiskPct:    3.0,
				Strategy:   struct {
					Kind   string         `yaml:"kind"`
					Params map[string]any `yaml:"params"`
				}{Kind: "noop"},
			},
		},
	}
	rc, err := BuildPortfolioRunConfig(cfg, nil, "", testLogger())
	require.NoError(t, err)
	require.Len(t, rc.Instruments, 1)
	assert.Equal(t, 3.0, rc.Instruments[0].RiskPct)
}

func TestBuildPortfolioRunConfig_UnknownStrategyReturnsError(t *testing.T) {
	cfg := &PortfolioConfig{
		Instruments: []portfolioInstrumentYAML{
			{
				Instrument: "EUR_USD",
				Timeframe:  "H1",
				Strategy:   struct {
					Kind   string         `yaml:"kind"`
					Params map[string]any `yaml:"params"`
				}{Kind: "no-such-strategy"},
			},
		},
	}
	_, err := BuildPortfolioRunConfig(cfg, nil, "", testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strategy")
}

func TestBuildPortfolioRunConfig_InvalidTickIntervalReturnsError(t *testing.T) {
	cfg := &PortfolioConfig{
		Instruments: []portfolioInstrumentYAML{
			{
				Instrument:   "EUR_USD",
				Timeframe:    "H1",
				TickInterval: "not-a-duration",
				Strategy:     struct {
					Kind   string         `yaml:"kind"`
					Params map[string]any `yaml:"params"`
				}{Kind: "noop"},
			},
		},
	}
	_, err := BuildPortfolioRunConfig(cfg, nil, "", testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tick_interval")
}

func TestBuildPortfolioRunConfig_LiveStrategy(t *testing.T) {
	cfg := &PortfolioConfig{
		RiskPct: 1.0,
		Instruments: []portfolioInstrumentYAML{
			{
				Instrument: "EUR_USD",
				Timeframe:  "H1",
				Strategy:   struct {
					Kind   string         `yaml:"kind"`
					Params map[string]any `yaml:"params"`
				}{Kind: "pulse"},
			},
		},
	}
	rc, err := BuildPortfolioRunConfig(cfg, nil, "", testLogger())
	require.NoError(t, err)
	require.Len(t, rc.Instruments, 1)
	assert.Equal(t, "EUR_USD", rc.Instruments[0].Instrument)
}
