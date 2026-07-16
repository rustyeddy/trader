package signalreplay

import (
	"os"
	"testing"

	"github.com/rustyeddy/trader/backtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func goldenOpts() GenOptions {
	opts := DefaultGenOptions()
	opts.SignalsPath = "testdata/gen_fixture.csv"
	opts.ExitKind = "chandelier"
	opts.ExitParams = map[string]any{"atr_period": int64(14), "multiplier": 2.0}
	return opts
}

func TestGenerateConfig_GoldenYAML(t *testing.T) {
	cfg, err := GenerateConfig(goldenOpts())
	require.NoError(t, err)

	got, err := yaml.Marshal(cfg)
	require.NoError(t, err)

	want, err := os.ReadFile("testdata/golden_replay.yml")
	require.NoError(t, err)

	assert.Equal(t, string(want), string(got))
}

func TestGenerateConfig_DeterministicAcrossRuns(t *testing.T) {
	cfg1, err := GenerateConfig(goldenOpts())
	require.NoError(t, err)
	cfg2, err := GenerateConfig(goldenOpts())
	require.NoError(t, err)

	b1, err := yaml.Marshal(cfg1)
	require.NoError(t, err)
	b2, err := yaml.Marshal(cfg2)
	require.NoError(t, err)
	assert.Equal(t, string(b1), string(b2))
}

func TestGenerateConfig_OneRunPerDistinctInstrument(t *testing.T) {
	cfg, err := GenerateConfig(goldenOpts())
	require.NoError(t, err)
	require.Len(t, cfg.Runs, 2)
	assert.Equal(t, "EURUSD", cfg.Runs[0].Data.Instrument)
	assert.Equal(t, "GBPUSD", cfg.Runs[1].Data.Instrument)
}

func TestGenerateConfig_DateRangeIncludesWarmupAndRunout(t *testing.T) {
	opts := goldenOpts()
	opts.WarmupDays = 10
	opts.RunoutDays = 20
	cfg, err := GenerateConfig(opts)
	require.NoError(t, err)

	eur := cfg.Runs[0]
	// EURUSD tradeable rows: 2024-01-02, 2024-01-03, 2024-03-01 (hot row skipped).
	assert.Equal(t, "2023-12-23", eur.Data.From) // 2024-01-02 - 10d
	assert.Equal(t, "2024-03-21", eur.Data.To)   // 2024-03-01 + 20d
}

func TestGenerateConfig_RefusesEmptyExit(t *testing.T) {
	opts := goldenOpts()
	opts.ExitKind = ""
	_, err := GenerateConfig(opts)
	assert.ErrorContains(t, err, "--exit")
}

func TestGenerateConfig_RequiresSignalsPath(t *testing.T) {
	opts := goldenOpts()
	opts.SignalsPath = ""
	_, err := GenerateConfig(opts)
	assert.ErrorContains(t, err, "--signals")
}

func TestGenerateConfig_ErrorsOnNoTradeableRows(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.csv"
	require.NoError(t, os.WriteFile(path, []byte("DATE,PAIR,BUCKET,BIAS\n2024-01-01T00:00:00Z,EURUSD,hot,long\n"), 0o644))

	opts := goldenOpts()
	opts.SignalsPath = path
	_, err := GenerateConfig(opts)
	assert.ErrorContains(t, err, "no tradeable rows")
}

func TestGenerateConfig_ProducesValidBacktestConfig(t *testing.T) {
	cfg, err := GenerateConfig(goldenOpts())
	require.NoError(t, err)

	b, err := yaml.Marshal(cfg)
	require.NoError(t, err)

	dir := t.TempDir()
	path := dir + "/replay.yml"
	require.NoError(t, os.WriteFile(path, b, 0o644))

	loaded, err := backtest.LoadConfig(path)
	require.NoError(t, err)
	assert.Len(t, loaded.Runs, 2)
}

// ── ParseExitParams ───────────────────────────────────────────────────────

func TestParseExitParams_InfersTypes(t *testing.T) {
	params, err := ParseExitParams("atr_period=14,multiplier=2.5,trail=true,label=foo")
	require.NoError(t, err)
	assert.Equal(t, int64(14), params["atr_period"])
	assert.Equal(t, 2.5, params["multiplier"])
	assert.Equal(t, true, params["trail"])
	assert.Equal(t, "foo", params["label"])
}

func TestParseExitParams_Empty(t *testing.T) {
	params, err := ParseExitParams("")
	require.NoError(t, err)
	assert.Nil(t, params)
}

func TestParseExitParams_InvalidEntry(t *testing.T) {
	_, err := ParseExitParams("atr_period")
	assert.ErrorContains(t, err, "invalid")
}

func TestParseExitParams_EmptyKey(t *testing.T) {
	_, err := ParseExitParams("=14")
	assert.ErrorContains(t, err, "empty key")
}
