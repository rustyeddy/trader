package trader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_YAML(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "cfg.yaml")

	yaml := `
version: 1
defaults:
  starting_balance: 10000.0
  account_ccy: USD
  scale: 100000
runs:
  - name: test-run
    data:
      instrument: EURUSD
      timeframe: H1
      from: "2026-01-01"
      to: "2026-01-31"
    strategy:
      kind: buy-first
`
	require.NoError(t, os.WriteFile(p, []byte(yaml), 0o644))

	cfg, err := LoadConfig(p)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, 1, cfg.Version)
	assert.Len(t, cfg.Runs, 1)
	assert.Equal(t, "test-run", cfg.Runs[0].Name)
}

func TestLoadConfig_YML(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "cfg.yml")

	yml := `
version: 1
defaults:
  starting_balance: 5000.0
  account_ccy: EUR
runs:
  - name: run-yml
    data:
      instrument: GBPUSD
      timeframe: M1
      from: "2026-02-01"
      to: "2026-02-28"
    strategy:
      kind: ema-cross
`
	require.NoError(t, os.WriteFile(p, []byte(yml), 0o644))

	cfg, err := LoadConfig(p)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "run-yml", cfg.Runs[0].Name)
}

func TestLoadConfig_JSON(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "cfg.json")

	json := `{
  "version": 1,
  "defaults": {
    "starting_balance": 10000.0,
    "account_ccy": "USD",
    "scale": 100000
  },
  "runs": [
    {
      "name": "json-run",
      "data": {
        "instrument": "USDJPY",
        "timeframe": "D1",
        "from": "2026-01-01",
        "to": "2026-12-31"
      },
      "strategy": {
        "kind": "simple"
      }
    }
  ]
}`
	require.NoError(t, os.WriteFile(p, []byte(json), 0o644))

	cfg, err := LoadConfig(p)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "json-run", cfg.Runs[0].Name)
}

func TestLoadConfig_DefaultsVersion(t *testing.T) {
	// version 0 should become 1
	tmp := t.TempDir()
	p := filepath.Join(tmp, "cfg.yaml")

	yaml := `
defaults:
  starting_balance: 1000.0
runs:
  - name: no-ver
    data:
      instrument: EURUSD
      timeframe: H1
      from: "2026-01-01"
      to: "2026-01-31"
    strategy:
      kind: buy-first
`
	require.NoError(t, os.WriteFile(p, []byte(yaml), 0o644))

	cfg, err := LoadConfig(p)
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version)
}

func TestLoadConfig_NotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	require.Error(t, err)
}

func TestLoadConfig_UnsupportedExt(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "cfg.toml")
	require.NoError(t, os.WriteFile(p, []byte(""), 0o644))

	_, err := LoadConfig(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported config extension")
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "cfg.yaml")
	require.NoError(t, os.WriteFile(p, []byte(":\tbad: [yaml"), 0o644))

	_, err := LoadConfig(p)
	require.Error(t, err)
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "cfg.json")
	require.NoError(t, os.WriteFile(p, []byte("{bad json"), 0o644))

	_, err := LoadConfig(p)
	require.Error(t, err)
}

func TestLoadConfig_NoRuns(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "cfg.yaml")
	yaml := `version: 1
defaults: {}
runs: []
`
	require.NoError(t, os.WriteFile(p, []byte(yaml), 0o644))

	_, err := LoadConfig(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no runs")
}

func validConfig() *Config {
	return &Config{
		Version: 1,
		Defaults: RunDefaults{
			StartingBalance: 10000.0,
			AccountCCY:      "USD",
			Scale:           100000,
			Units:           1000,
		},
		Runs: []RunConfig{
			{
				Name: "run-a",
				Data: DataConfig{
					Instrument: "EURUSD",
					Timeframe:  "H1",
					From:       "2026-01-01",
					To:         "2026-01-31",
				},
				Strategy: StrategyConfig{Kind: "buy-first"},
			},
			{
				Name: "run-b",
				Data: DataConfig{
					Instrument: "GBPUSD",
					Timeframe:  "M1",
					From:       "2026-02-01",
					To:         "2026-02-28",
				},
				Strategy: StrategyConfig{Kind: "ema-cross"},
			},
		},
	}
}

func TestParseTimeframe_FromBacktestConfigCoverage(t *testing.T) {
	tests := []struct {
		in   string
		want Timeframe
		err  bool
	}{
		{"M1", M1, false},
		{"m1", M1, false},
		{"H1", H1, false},
		{"h1", H1, false},
		{"D1", D1, false},
		{"d1", D1, false},
		{"W1", 0, true},
		{"", 0, true},
	}
	for _, tc := range tests {
		got, err := ParseTimeframe(tc.in)
		if tc.err {
			assert.Error(t, err, "input=%q", tc.in)
		} else {
			require.NoError(t, err, "input=%q", tc.in)
			assert.Equal(t, tc.want, got, "input=%q", tc.in)
		}
	}
}

// ─── firstNonEmpty ────────────────────────────────────────────────────────────

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "a", firstNonEmpty("a", "b"))
	assert.Equal(t, "b", firstNonEmpty("", "b"))
	assert.Equal(t, "b", firstNonEmpty("  ", "b"))
	assert.Equal(t, "c", firstNonEmpty("", "", "c"))
	assert.Equal(t, "", firstNonEmpty("", ""))
	assert.Equal(t, "", firstNonEmpty())
}

// ─── ApplyCommonParamOverrides ────────────────────────────────────────────────

// ─── GetInt32Param ────────────────────────────────────────────────────────────

func TestGetInt32Param(t *testing.T) {
	m := map[string]any{
		"int":     42,
		"int32":   int32(10),
		"int64":   int64(20),
		"float64": float64(30),
		"bad":     "string",
	}

	v, ok, err := GetInt32Param(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), v)

	v, ok, err = GetInt32Param(m, "int")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(42), v)

	v, ok, err = GetInt32Param(m, "int32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(10), v)

	v, ok, err = GetInt32Param(m, "int64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(20), v)

	v, ok, err = GetInt32Param(m, "float64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(30), v)

	_, ok, err = GetInt32Param(m, "bad")
	assert.True(t, ok)
	assert.Error(t, err)
}

// ─── GetFloat64Param ──────────────────────────────────────────────────────────

func TestGetFloat64Param(t *testing.T) {
	m := map[string]any{
		"float64": float64(1.5),
		"float32": float32(2.5),
		"int":     int(3),
		"int32":   int32(4),
		"int64":   int64(5),
		"bad":     "string",
	}

	v, ok, err := GetFloat64Param(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, v)

	v, ok, err = GetFloat64Param(m, "float64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.InDelta(t, 1.5, v, 1e-9)

	v, ok, err = GetFloat64Param(m, "float32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.InDelta(t, 2.5, v, 1e-4)

	v, ok, err = GetFloat64Param(m, "int")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 3.0, v)

	v, ok, err = GetFloat64Param(m, "int32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 4.0, v)

	v, ok, err = GetFloat64Param(m, "int64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 5.0, v)

	_, ok, err = GetFloat64Param(m, "bad")
	assert.True(t, ok)
	assert.Error(t, err)
}
