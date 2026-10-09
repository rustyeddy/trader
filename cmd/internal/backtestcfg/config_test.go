package backtestcfg

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/config"
	"github.com/rustyeddy/trader/internal/strategy/emacross"
	"github.com/rustyeddy/trader/num"
)

// issue247CandidateYAML is #247's own candidate YAML, verbatim.
const issue247CandidateYAML = `
backtest:
  symbol: EURUSD
  interval: H1
  from: 2015-01-01
  to: 2025-01-01
  starting_capital: 100000
  risk_fraction: 0.01
  adverse_distance: 0.0050

strategy:
  name: ema-cross
  fast_period: 20
  slow_period: 50
`

func loadRunConfig(t *testing.T, yaml string, overrides map[string]string) (RunConfig, error) {
	t.Helper()
	return config.Load[RunConfig](config.Options{
		Environ:     []string{},
		FileContent: []byte(yaml),
		Overrides:   overrides,
	})
}

func TestRunConfig_ParsesIssue247CandidateYAML(t *testing.T) {
	cfg, err := loadRunConfig(t, issue247CandidateYAML, nil)
	require.NoError(t, err)

	assert.Equal(t, "EURUSD", cfg.Backtest.Symbol)
	assert.Equal(t, "H1", cfg.Backtest.Interval)
	assert.Equal(t, "2015-01-01", cfg.Backtest.From)
	assert.Equal(t, "2025-01-01", cfg.Backtest.To)
	assert.Equal(t, "USD", cfg.Backtest.Currency) // not in the YAML: default applies
	assert.Equal(t, "100000", cfg.Backtest.StartingCapital)
	assert.Equal(t, "0.01", cfg.Backtest.RiskFraction.String())
	assert.Equal(t, "0.005", cfg.Backtest.AdverseDistance.String())

	assert.Equal(t, "ema-cross", cfg.Strategy.Name)
	assert.Equal(t, 20, cfg.Strategy.FastPeriod)
	assert.Equal(t, 50, cfg.Strategy.SlowPeriod)
}

func TestRunConfig_Defaults(t *testing.T) {
	const minimal = `
backtest:
  symbol: EURUSD
  from: 2015-01-01
  to: 2025-01-01
  adverse_distance: 0.0050
`
	cfg, err := loadRunConfig(t, minimal, nil)
	require.NoError(t, err)

	assert.Equal(t, "H1", cfg.Backtest.Interval)
	assert.Equal(t, "USD", cfg.Backtest.Currency)
	assert.Equal(t, "10000", cfg.Backtest.StartingCapital)
	assert.Equal(t, "0.01", cfg.Backtest.RiskFraction.String())
	assert.Equal(t, "buy-and-hold", cfg.Strategy.Name)
	assert.Equal(t, 20, cfg.Strategy.FastPeriod)
	assert.Equal(t, 50, cfg.Strategy.SlowPeriod)
	assert.Equal(t, "/srv/trading/data/canonical", cfg.Backtest.DataStoreRoot,
		"issue #268: canonical data persists by default rather than rebuilding into a fresh temporary directory every run")
	assert.Equal(t, emacross.SideBoth, cfg.Strategy.AllowedSide,
		"issue #273: default must reproduce EMA-01's own unrestricted reference behavior")
}

// TestRunConfig_DataStoreRootExplicitEmptyOverridesDefault proves an
// explicit empty --data-store-root (what every hermetic test in this
// package relies on) actually reaches RunConfig as "", not the
// default: config.Load's override lookup is a map "found" check, not
// an emptiness check, so an explicitly-supplied empty string must
// still count as an override rather than falling through to
// DataStoreRoot's own default tag (issue #268).
func TestRunConfig_DataStoreRootExplicitEmptyOverridesDefault(t *testing.T) {
	const minimal = `
backtest:
  symbol: EURUSD
  from: 2015-01-01
  to: 2025-01-01
  adverse_distance: 0.0050
`
	cfg, err := loadRunConfig(t, minimal, map[string]string{"data-store-root": ""})
	require.NoError(t, err)
	assert.Empty(t, cfg.Backtest.DataStoreRoot)
}

func TestRunConfig_MissingRequiredFieldFails(t *testing.T) {
	const missingFrom = `
backtest:
  symbol: EURUSD
  to: 2025-01-01
  adverse_distance: 0.0050
`
	_, err := loadRunConfig(t, missingFrom, nil)
	require.Error(t, err)
	// Checked in RunConfig.Validate (a model config legitimately omits
	// from), so the sentinel is flattened into the validation message.
	assert.ErrorIs(t, err, config.ErrValidation)
	assert.ErrorContains(t, err, "backtest.from")
	assert.ErrorContains(t, err, config.ErrRequired.Error())
}

func TestRunConfig_ValidateRejectsInvalidRelationships(t *testing.T) {
	base := func(overrides map[string]string) map[string]string {
		m := map[string]string{
			"symbol":           "EURUSD",
			"from":             "2015-01-01",
			"to":               "2025-01-01",
			"adverse-distance": "0.0050",
			"strategy-name":    "ema-cross",
		}
		maps.Copy(m, overrides)
		return m
	}

	cases := []struct {
		name      string
		overrides map[string]string
		wantErr   string
	}{
		{
			name:      "slow period not greater than fast period",
			overrides: base(map[string]string{"fast-period": "20", "slow-period": "20"}),
			wantErr:   "slow_period",
		},
		{
			name:      "fast period not positive",
			overrides: base(map[string]string{"fast-period": "0", "slow-period": "50"}),
			wantErr:   "fast_period",
		},
		{
			name:      "to not after from",
			overrides: base(map[string]string{"from": "2025-01-01", "to": "2015-01-01"}),
			wantErr:   "backtest.to",
		},
		{
			name:      "invalid interval",
			overrides: base(map[string]string{"interval": "H2"}),
			wantErr:   "backtest.interval",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load[RunConfig](config.Options{
				Environ:   []string{},
				Overrides: tc.overrides,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestRunConfig_OverrideBeatsFile proves explicit CLI overrides win
// over a conflicting config-file value (CONTRIBUTING.org's "explicit
// CLI override > config file > documented defaults" precedence,
// already implemented generically by config.Load — this test exists
// to prove RunConfig's own tags wire into that precedence correctly).
func TestRunConfig_OverrideBeatsFile(t *testing.T) {
	cfg, err := loadRunConfig(t, issue247CandidateYAML, map[string]string{
		"fast-period": "10",
	})
	require.NoError(t, err)

	assert.Equal(t, 10, cfg.Strategy.FastPeriod)
	assert.Equal(t, 50, cfg.Strategy.SlowPeriod) // untouched by the override
}

// TestRunConfig_EquivalentEffectiveConfigFromEitherSource proves
// #247's own acceptance criterion directly: a config sourced entirely
// from a YAML file and an equivalent one sourced entirely from
// overrides produce an identical effective RunConfig — and therefore
// an identical Manifest/ConfigDigest downstream, since run.go passes
// these same fields straight through to backtest.NewManifest without
// further transformation.
func TestRunConfig_EquivalentEffectiveConfigFromEitherSource(t *testing.T) {
	fromFile, err := loadRunConfig(t, issue247CandidateYAML, nil)
	require.NoError(t, err)

	fromOverrides, err := config.Load[RunConfig](config.Options{
		Environ: []string{},
		Overrides: map[string]string{
			"symbol":           "EURUSD",
			"interval":         "H1",
			"from":             "2015-01-01",
			"to":               "2025-01-01",
			"starting-cash":    "100000",
			"risk-fraction":    "0.01",
			"adverse-distance": "0.0050",
			"strategy-name":    "ema-cross",
			"fast-period":      "20",
			"slow-period":      "50",
		},
	})
	require.NoError(t, err)

	assert.Equal(t, fromFile, fromOverrides)
}

// TestRunConfig_ExternalStrategyKeys covers the issue #469 keys.
func TestRunConfig_ExternalStrategyKeys(t *testing.T) {
	load := func(overrides map[string]string) (RunConfig, error) {
		m := map[string]string{
			"from": "2015-01-01", "to": "2025-01-01", "adverse-distance": "0.0050",
		}
		maps.Copy(m, overrides)
		return config.Load[RunConfig](config.Options{Environ: []string{}, Overrides: m})
	}

	t.Run("exec skips the in-process strategy registry", func(t *testing.T) {
		cfg, err := load(map[string]string{"strategy-exec": "./scanner", "strategy-name": "some-external-name", "symbols": "EURUSD,GBPUSD"})
		require.NoError(t, err)
		assert.Equal(t, "./scanner", cfg.Strategy.Exec)
		assert.Equal(t, "EURUSD,GBPUSD", cfg.Backtest.Symbols)
	})
	t.Run("an unregistered name still fails without exec", func(t *testing.T) {
		_, err := load(map[string]string{"strategy-name": "some-external-name"})
		require.ErrorContains(t, err, "not registered")
	})
	t.Run("symbol and symbols are mutually exclusive", func(t *testing.T) {
		_, err := load(map[string]string{"symbol": "EURUSD", "symbols": "GBPUSD"})
		require.ErrorContains(t, err, "mutually exclusive")
	})
	t.Run("strategy config requires exec", func(t *testing.T) {
		_, err := load(map[string]string{"strategy-config": "x.yml"})
		require.ErrorContains(t, err, "strategy.config requires strategy.exec")
	})
	t.Run("exec and config are not strategy parameters", func(t *testing.T) {
		cfg, err := load(map[string]string{"strategy-exec": "./scanner", "strategy-config": "x.yml"})
		require.NoError(t, err)
		b, err := json.Marshal(cfg.Strategy)
		require.NoError(t, err)
		assert.NotContains(t, string(b), "scanner")
		assert.NotContains(t, string(b), "x.yml")
	})
}

func TestRun_ExecWithoutExternalStrategyRejected(t *testing.T) {
	cfg := RunConfig{}
	cfg.Backtest.From, cfg.Backtest.To = "2024-01-01", "2024-02-01"
	cfg.Backtest.Interval = "D1"
	cfg.Backtest.InitialMarginRatio = num.MustParseRate("1")
	cfg.Strategy.Exec = "./scanner"
	_, err := run(context.Background(), Request{Config: cfg, Symbols: []string{"EURUSD"}})
	require.ErrorIs(t, err, ErrInvalidRun)
	require.ErrorContains(t, err, "no ExternalStrategy")
}

const modelYAML = `
model:
  symbols: >-
    EURUSD, GBPUSD,
    USDJPY
  interval: D1
  from: 2020-01-01T00:00:00Z
  to: 2024-12-31T00:00:00Z
  currency: USD
  starting_capital: 10000
strategy:
  exec: ./scanner
`

func loadModel(t *testing.T, yaml string, overrides map[string]string) (RunConfig, error) {
	t.Helper()
	return LoadRunConfig(config.Options{Environ: []string{}, FileContent: []byte(yaml), Overrides: overrides})
}

// TestModelConfig covers issue #471: a model config needs only its six
// keys (no risk_fraction/adverse_distance), and resolves onto Backtest.
func TestModelConfig(t *testing.T) {
	t.Run("minimal model resolves onto backtest", func(t *testing.T) {
		cfg, err := loadModel(t, modelYAML, nil)
		require.NoError(t, err)
		assert.Contains(t, cfg.Backtest.Symbols, "GBPUSD")
		assert.Equal(t, "D1", cfg.Backtest.Interval)
		assert.Equal(t, "2020-01-01T00:00:00Z", cfg.Backtest.From)
		assert.Equal(t, "2024-12-31T00:00:00Z", cfg.Backtest.To)
		assert.Equal(t, "USD", cfg.Backtest.Currency)
		assert.Equal(t, "10000", cfg.Backtest.StartingCapital)
	})
	t.Run("backtest flags still override model values", func(t *testing.T) {
		cfg, err := loadModel(t, modelYAML, map[string]string{"from": "2021-01-01", "symbol": "EURUSD", "symbols": ""})
		require.NoError(t, err)
		assert.Equal(t, "2021-01-01", cfg.Backtest.From)
		assert.Equal(t, "EURUSD", cfg.Backtest.Symbol)
		assert.Empty(t, cfg.Backtest.Symbols)
	})
	t.Run("every model key is required", func(t *testing.T) {
		for _, key := range []string{"symbols", "interval", "from", "to", "currency", "starting_capital"} {
			var kept []string
			for _, line := range strings.Split(modelYAML, "\n") {
				if strings.HasPrefix(line, "  "+key+":") {
					continue
				}
				kept = append(kept, line)
			}
			yaml := strings.Join(kept, "\n")
			if key == "symbols" { // the folded scalar's continuation lines
				yaml = strings.ReplaceAll(yaml, "    EURUSD, GBPUSD,\n    USDJPY\n", "")
			}
			_, err := loadModel(t, yaml, nil)
			require.Error(t, err, key)
			assert.ErrorContains(t, err, "model."+key)
		}
	})
	t.Run("model requires strategy.exec", func(t *testing.T) {
		_, err := loadModel(t, strings.Replace(modelYAML, "strategy:\n  exec: ./scanner\n", "", 1), nil)
		require.ErrorContains(t, err, "model requires strategy.exec")
	})
	t.Run("adverse_distance is rejected beside a model", func(t *testing.T) {
		_, err := loadModel(t, modelYAML+"backtest:\n  adverse_distance: 0.01\n", nil)
		require.ErrorContains(t, err, "adverse_distance does not apply")
	})
	t.Run("a backtest config still requires adverse_distance", func(t *testing.T) {
		_, err := loadModel(t, "backtest:\n  symbol: EURUSD\n  from: 2024-01-01\n  to: 2024-02-01\n", nil)
		require.ErrorIs(t, err, config.ErrRequired)
		assert.ErrorContains(t, err, "backtest.adverse_distance")
	})
}
