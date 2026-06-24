package trader

import (
	"testing"

	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashBacktestConfig_Length(t *testing.T) {
	cfg := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2024-01-01", To: "2024-03-31"},
		Strategy: strategy.StrategyConfig{Kind: "ema-cross", Params: map[string]any{"fast": 9, "slow": 21}},
	}
	h := hashBacktestConfig(cfg, RunDefaults{})
	assert.Len(t, h, 8, "hash must be 8 hex chars")
}

func TestHashBacktestConfig_Stable(t *testing.T) {
	cfg := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2024-01-01", To: "2024-03-31"},
		Strategy: strategy.StrategyConfig{Kind: "ema-cross", Params: map[string]any{"fast": 9, "slow": 21}},
	}
	assert.Equal(t, hashBacktestConfig(cfg, RunDefaults{}), hashBacktestConfig(cfg, RunDefaults{}), "same config must produce same hash")
}

func TestHashBacktestConfig_ParamChange(t *testing.T) {
	base := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1"},
		Strategy: strategy.StrategyConfig{Kind: "ema-cross", Params: map[string]any{"fast": 9, "slow": 21}},
	}
	changed := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1"},
		Strategy: strategy.StrategyConfig{Kind: "ema-cross", Params: map[string]any{"fast": 9, "slow": 50}},
	}
	assert.NotEqual(t, hashBacktestConfig(base, RunDefaults{}), hashBacktestConfig(changed, RunDefaults{}), "different params must produce different hash")
}

func TestHashBacktestConfig_InstrumentChange(t *testing.T) {
	eur := RunConfig{Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1"}}
	usd := RunConfig{Data: DataConfig{Instrument: "USDJPY", Timeframe: "H1"}}
	assert.NotEqual(t, hashBacktestConfig(eur, RunDefaults{}), hashBacktestConfig(usd, RunDefaults{}))
}

func TestHashBacktestConfig_NameIgnored(t *testing.T) {
	// Name is a label; changing it must not change the hash.
	a := RunConfig{Name: "run-v1", Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1"}}
	b := RunConfig{Name: "run-v2", Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1"}}
	assert.Equal(t, hashBacktestConfig(a, RunDefaults{}), hashBacktestConfig(b, RunDefaults{}), "name change must not affect hash")
}

func TestCompileBacktests_SetsConfigHash(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 10000, RiskPct: 1.0, Source: "oanda"},
		Runs: []RunConfig{
			{
				Name:     "test-run",
				Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2024-01-01", To: "2024-03-31"},
				Strategy: strategy.StrategyConfig{Kind: "fake"},
			},
		},
	}

	runs, err := CompileBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	hash := runs[0].Request.ConfigHash
	assert.Len(t, hash, 8)

	// Re-running CompileBacktests with the same config must produce the same hash.
	runs2, err := CompileBacktests(cfg)
	require.NoError(t, err)
	assert.Equal(t, hash, runs2[0].Request.ConfigHash)
}

func TestCompileBacktests_StoresResolvedRunConfig(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{Source: "oanda"},
		Runs: []RunConfig{{
			Name:     "test-run",
			Data:     DataConfig{Instrument: "GBPUSD", Timeframe: "D1", From: "2023-01-01", To: "2023-12-31"},
			Strategy: strategy.StrategyConfig{Kind: "fake"},
		}},
	}

	runs, err := CompileBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "oanda", runs[0].RunConfig.Data.Source)
	assert.Equal(t, runs[0].Request.ConfigHash, hashBacktestConfig(runs[0].RunConfig, cfg.Defaults))
}

func TestCompileBacktests_StoresRunConfig(t *testing.T) {
	rc := RunConfig{
		Name:     "test-run",
		Data:     DataConfig{Instrument: "GBPUSD", Timeframe: "D1", From: "2023-01-01", To: "2023-12-31"},
		Strategy: strategy.StrategyConfig{Kind: "fake"},
	}
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 5000, RiskPct: 0.5, Source: "oanda"},
		Runs:     []RunConfig{rc},
	}

	runs, err := CompileBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	assert.Equal(t, rc.Data.Instrument, runs[0].RunConfig.Data.Instrument)
	assert.Equal(t, rc.Data.Timeframe, runs[0].RunConfig.Data.Timeframe)
	assert.Equal(t, rc.Strategy.Kind, runs[0].RunConfig.Strategy.Kind)
}

func TestHashBacktestConfig_DefaultsChangeHash(t *testing.T) {
	cfg := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2024-01-01", To: "2024-03-31"},
		Strategy: strategy.StrategyConfig{Kind: "fake"},
	}
	a := RunDefaults{StartingBalance: 10000, RiskPct: 1.0, SlippagePips: 0.5}
	b := RunDefaults{StartingBalance: 20000, RiskPct: 1.0, SlippagePips: 0.5}
	assert.NotEqual(t, hashBacktestConfig(cfg, a), hashBacktestConfig(cfg, b))
}
