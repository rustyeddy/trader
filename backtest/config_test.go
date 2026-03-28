package backtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── LoadConfig ──────────────────────────────────────────────────────────────

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

// ─── ResolveRun / ResolveAllRuns ─────────────────────────────────────────────

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

func TestResolveRun_Found(t *testing.T) {
	cfg := validConfig()
	rr, err := cfg.ResolveRun("run-a")
	require.NoError(t, err)
	assert.Equal(t, "run-a", rr.Name)
	assert.Equal(t, "EURUSD", rr.Instrument)
}

func TestResolveRun_NotFound(t *testing.T) {
	cfg := validConfig()
	_, err := cfg.ResolveRun("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveAllRuns(t *testing.T) {
	cfg := validConfig()
	runs, err := cfg.ResolveAllRuns()
	require.NoError(t, err)
	assert.Len(t, runs, 2)
	assert.Equal(t, "run-a", runs[0].Name)
	assert.Equal(t, "run-b", runs[1].Name)
}

// ─── resolve validation errors ───────────────────────────────────────────────

func TestResolve_MissingName(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000},
		Runs: []RunConfig{
			{
				Name: "   ",
				Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-31"},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	_, err := cfg.ResolveAllRuns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing run name")
}

func TestResolve_MissingInstrument(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000},
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{Instrument: "", Timeframe: "H1", From: "2026-01-01", To: "2026-01-31"},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	_, err := cfg.ResolveAllRuns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing data.instrument")
}

func TestResolve_MissingTimeframe(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000},
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{Instrument: "EURUSD", Timeframe: "", From: "2026-01-01", To: "2026-01-31"},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	_, err := cfg.ResolveAllRuns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing data.timeframe")
}

func TestResolve_MissingFrom(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000},
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "", To: "2026-01-31"},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	_, err := cfg.ResolveAllRuns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing data.from")
}

func TestResolve_MissingTo(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000},
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: ""},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	_, err := cfg.ResolveAllRuns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing data.to")
}

func TestResolve_MissingStrategy(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000},
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-31"},
				Strategy: StrategyConfig{Kind: ""},
			},
		},
	}
	_, err := cfg.ResolveAllRuns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing strategy.kind")
}

func TestResolve_DefaultScaleFallback(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000, Scale: 0}, // 0 → PriceScale
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-31"},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	rr, err := cfg.ResolveAllRuns()
	require.NoError(t, err)
	assert.Equal(t, types.PriceScale, rr[0].Scale)
}

func TestResolve_DataStrictOverride(t *testing.T) {
	strict := true
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000, Strict: false},
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-31", Strict: &strict},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	rr, err := cfg.ResolveAllRuns()
	require.NoError(t, err)
	assert.True(t, rr[0].Strict)
}

func TestResolve_SourceFallback(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000, Source: "default-src"},
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{
					Instrument: "EURUSD",
					Timeframe:  "H1",
					From:       "2026-01-01",
					To:         "2026-01-31",
					// Source empty → falls back to defaults
				},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	rr, err := cfg.ResolveAllRuns()
	require.NoError(t, err)
	assert.Equal(t, "default-src", rr[0].Source)
}

func TestResolve_DataSourceOverridesDefault(t *testing.T) {
	cfg := &Config{
		Defaults: RunDefaults{StartingBalance: 1000, Source: "default-src"},
		Runs: []RunConfig{
			{
				Name: "run",
				Data: DataConfig{
					Instrument: "EURUSD",
					Timeframe:  "H1",
					From:       "2026-01-01",
					To:         "2026-01-31",
					Source:     "data-src",
				},
				Strategy: StrategyConfig{Kind: "x"},
			},
		},
	}
	rr, err := cfg.ResolveAllRuns()
	require.NoError(t, err)
	assert.Equal(t, "data-src", rr[0].Source)
}

// ─── CandleRequest ────────────────────────────────────────────────────────────

func TestCandleRequest_Valid(t *testing.T) {
	rr := &ResolvedRun{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2026-01-01",
		To:         "2026-01-31",
	}
	cr, err := rr.CandleRequest()
	require.NoError(t, err)
	assert.Equal(t, "EURUSD", cr.Instrument)
	assert.Equal(t, types.H1, cr.Timeframe)
	assert.False(t, cr.Range.Start.IsZero())
	assert.False(t, cr.Range.End.IsZero())
}

func TestCandleRequest_InvalidTimeframe(t *testing.T) {
	rr := &ResolvedRun{
		Instrument: "EURUSD",
		Timeframe:  "INVALID",
		From:       "2026-01-01",
		To:         "2026-01-31",
	}
	_, err := rr.CandleRequest()
	require.Error(t, err)
}

func TestCandleRequest_InvalidFrom(t *testing.T) {
	rr := &ResolvedRun{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "not-a-date",
		To:         "2026-01-31",
	}
	_, err := rr.CandleRequest()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad from")
}

func TestCandleRequest_InvalidTo(t *testing.T) {
	rr := &ResolvedRun{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2026-01-01",
		To:         "not-a-date",
	}
	_, err := rr.CandleRequest()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad to")
}

// ─── parseTimeframe ───────────────────────────────────────────────────────────

func TestParseTimeframe(t *testing.T) {
	tests := []struct {
		in   string
		want types.Timeframe
		err  bool
	}{
		{"M1", types.M1, false},
		{"m1", types.M1, false},
		{"H1", types.H1, false},
		{"h1", types.H1, false},
		{"D1", types.D1, false},
		{"d1", types.D1, false},
		{"W1", 0, true},
		{"", 0, true},
	}
	for _, tc := range tests {
		got, err := parseTimeframe(tc.in)
		if tc.err {
			assert.Error(t, err, "input=%q", tc.in)
		} else {
			require.NoError(t, err, "input=%q", tc.in)
			assert.Equal(t, tc.want, got, "input=%q", tc.in)
		}
	}
}

// ─── parseDateStart / parseDateEndExclusive ───────────────────────────────────

func TestParseDateStart_Valid(t *testing.T) {
	tm, err := parseDateStart("2026-03-15")
	require.NoError(t, err)
	assert.Equal(t, 2026, tm.Year())
	assert.Equal(t, 15, tm.Day())
}

func TestParseDateStart_Invalid(t *testing.T) {
	_, err := parseDateStart("15/03/2026")
	require.Error(t, err)
}

func TestParseDateEndExclusive_Valid(t *testing.T) {
	tm, err := parseDateEndExclusive("2026-03-15")
	require.NoError(t, err)
	// end-exclusive adds one day
	assert.Equal(t, 16, tm.Day())
}

func TestParseDateEndExclusive_Invalid(t *testing.T) {
	_, err := parseDateEndExclusive("bad-date")
	require.Error(t, err)
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

// ─── percentToRate ────────────────────────────────────────────────────────────

func TestPercentToRate(t *testing.T) {
	r := percentToRate(1.0)
	assert.Equal(t, types.RateFromFloat(0.01), r)

	r = percentToRate(0.5)
	assert.Equal(t, types.RateFromFloat(0.005), r)
}

// ─── ApplyCommonParamOverrides ────────────────────────────────────────────────

func TestApplyCommonParamOverrides_NoParams(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: nil},
	}
	require.NoError(t, rr.ApplyCommonParamOverrides())
}

func TestApplyCommonParamOverrides_Units(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: map[string]any{"units": 5000}},
	}
	require.NoError(t, rr.ApplyCommonParamOverrides())
	assert.Equal(t, types.Units(5000), rr.Units)
}

func TestApplyCommonParamOverrides_StopAndTakePips(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: map[string]any{
			"stop_pips": int32(20),
			"take_pips": float64(40),
		}},
	}
	require.NoError(t, rr.ApplyCommonParamOverrides())
	assert.Equal(t, types.Price(20), rr.StopPips)
	assert.Equal(t, types.Price(40), rr.TakePips)
}

func TestApplyCommonParamOverrides_RiskAndRR(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: map[string]any{
			"risk_pct": float64(1.0),
			"rr":       float64(2.0),
		}},
	}
	require.NoError(t, rr.ApplyCommonParamOverrides())
	assert.Equal(t, percentToRate(1.0), rr.RiskPct)
	assert.Equal(t, types.RateFromFloat(2.0), rr.RR)
}

func TestApplyCommonParamOverrides_BadUnits(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: map[string]any{"units": "bad"}},
	}
	require.Error(t, rr.ApplyCommonParamOverrides())
}

func TestApplyCommonParamOverrides_BadStopPips(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: map[string]any{"stop_pips": "bad"}},
	}
	require.Error(t, rr.ApplyCommonParamOverrides())
}

func TestApplyCommonParamOverrides_BadTakePips(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: map[string]any{"take_pips": "bad"}},
	}
	require.Error(t, rr.ApplyCommonParamOverrides())
}

func TestApplyCommonParamOverrides_BadRiskPct(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: map[string]any{"risk_pct": "bad"}},
	}
	require.Error(t, rr.ApplyCommonParamOverrides())
}

func TestApplyCommonParamOverrides_BadRR(t *testing.T) {
	rr := &ResolvedRun{
		Strategy: StrategyConfig{Params: map[string]any{"rr": "bad"}},
	}
	require.Error(t, rr.ApplyCommonParamOverrides())
}

// ─── getInt32Param ────────────────────────────────────────────────────────────

func TestGetInt32Param(t *testing.T) {
	m := map[string]any{
		"int":     42,
		"int32":   int32(10),
		"int64":   int64(20),
		"float64": float64(30),
		"bad":     "string",
	}

	v, ok, err := getInt32Param(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), v)

	v, ok, err = getInt32Param(m, "int")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(42), v)

	v, ok, err = getInt32Param(m, "int32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(10), v)

	v, ok, err = getInt32Param(m, "int64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(20), v)

	v, ok, err = getInt32Param(m, "float64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, int32(30), v)

	_, ok, err = getInt32Param(m, "bad")
	assert.True(t, ok)
	assert.Error(t, err)
}

// ─── getFloat64Param ──────────────────────────────────────────────────────────

func TestGetFloat64Param(t *testing.T) {
	m := map[string]any{
		"float64": float64(1.5),
		"float32": float32(2.5),
		"int":     int(3),
		"int32":   int32(4),
		"int64":   int64(5),
		"bad":     "string",
	}

	v, ok, err := getFloat64Param(m, "missing")
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, v)

	v, ok, err = getFloat64Param(m, "float64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.InDelta(t, 1.5, v, 1e-9)

	v, ok, err = getFloat64Param(m, "float32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.InDelta(t, 2.5, v, 1e-4)

	v, ok, err = getFloat64Param(m, "int")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 3.0, v)

	v, ok, err = getFloat64Param(m, "int32")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 4.0, v)

	v, ok, err = getFloat64Param(m, "int64")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 5.0, v)

	_, ok, err = getFloat64Param(m, "bad")
	assert.True(t, ok)
	assert.Error(t, err)
}
