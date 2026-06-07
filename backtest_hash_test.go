package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashRunConfig_Length(t *testing.T) {
	cfg := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2024-01-01", To: "2024-03-31"},
		Strategy: StrategyConfig{Kind: "ema-cross", Params: map[string]any{"fast": 9, "slow": 21}},
	}
	h := hashRunConfig(cfg)
	assert.Len(t, h, 8, "hash must be 8 hex chars")
}

func TestHashRunConfig_Stable(t *testing.T) {
	cfg := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2024-01-01", To: "2024-03-31"},
		Strategy: StrategyConfig{Kind: "ema-cross", Params: map[string]any{"fast": 9, "slow": 21}},
	}
	assert.Equal(t, hashRunConfig(cfg), hashRunConfig(cfg), "same config must produce same hash")
}

func TestHashRunConfig_ParamChange(t *testing.T) {
	base := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1"},
		Strategy: StrategyConfig{Kind: "ema-cross", Params: map[string]any{"fast": 9, "slow": 21}},
	}
	changed := RunConfig{
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1"},
		Strategy: StrategyConfig{Kind: "ema-cross", Params: map[string]any{"fast": 9, "slow": 50}},
	}
	assert.NotEqual(t, hashRunConfig(base), hashRunConfig(changed), "different params must produce different hash")
}

func TestHashRunConfig_InstrumentChange(t *testing.T) {
	eur := RunConfig{Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1"}}
	usd := RunConfig{Data: DataConfig{Instrument: "USDJPY", Timeframe: "H1"}}
	assert.NotEqual(t, hashRunConfig(eur), hashRunConfig(usd))
}

func TestHashRunConfig_NameIgnored(t *testing.T) {
	// Name is a label; changing it must not change the hash.
	a := RunConfig{Name: "run-v1", Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1"}}
	b := RunConfig{Name: "run-v2", Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1"}}
	assert.Equal(t, hashRunConfig(a), hashRunConfig(b), "name change must not affect hash")
}

func TestGetBacktests_SetsConfigHash(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 10000, RiskPct: 1.0, Source: "oanda"},
		Runs: []RunConfig{
			{
				Name:     "test-run",
				Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2024-01-01", To: "2024-03-31"},
				Strategy: StrategyConfig{Kind: "fake"},
			},
		},
	}

	runs, err := GetBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	hash := runs[0].ConfigHash
	assert.Len(t, hash, 8)

	// Re-running GetBacktests with the same config must produce the same hash.
	runs2, err := GetBacktests(cfg)
	require.NoError(t, err)
	assert.Equal(t, hash, runs2[0].ConfigHash)
}

func TestCompileBacktests_StoresResolvedRunConfig(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{Source: "oanda"},
		Runs: []RunConfig{{
			Name:     "test-run",
			Data:     DataConfig{Instrument: "GBPUSD", Timeframe: "D1", From: "2023-01-01", To: "2023-12-31"},
			Strategy: StrategyConfig{Kind: "fake"},
		}},
	}

	runs, err := CompileBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "oanda", runs[0].RunConfig.Data.Source)
	assert.Equal(t, runs[0].Request.ConfigHash, hashRunConfig(runs[0].RunConfig))
}

func TestGetBacktests_StoresRunConfig(t *testing.T) {
	rc := RunConfig{
		Name:     "test-run",
		Data:     DataConfig{Instrument: "GBPUSD", Timeframe: "D1", From: "2023-01-01", To: "2023-12-31"},
		Strategy: StrategyConfig{Kind: "fake"},
	}
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 5000, RiskPct: 0.5, Source: "oanda"},
		Runs:     []RunConfig{rc},
	}

	runs, err := GetBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	assert.Equal(t, rc.Data.Instrument, runs[0].RunConfig.Data.Instrument)
	assert.Equal(t, rc.Data.Timeframe, runs[0].RunConfig.Data.Timeframe)
	assert.Equal(t, rc.Strategy.Kind, runs[0].RunConfig.Strategy.Kind)
}
