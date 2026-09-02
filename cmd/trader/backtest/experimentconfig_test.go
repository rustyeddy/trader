package backtest

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/config"
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

func loadRunConfig(t *testing.T, yaml string, overrides map[string]string) (runConfig, error) {
	t.Helper()
	return config.Load[runConfig](config.Options{
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
	assert.Equal(t, "ema-cross", cfg.Strategy.Name)
	assert.Equal(t, 20, cfg.Strategy.FastPeriod)
	assert.Equal(t, 50, cfg.Strategy.SlowPeriod)
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
	assert.ErrorIs(t, err, config.ErrRequired)
}

func TestRunConfig_ValidateRejectsInvalidRelationships(t *testing.T) {
	base := func(overrides map[string]string) map[string]string {
		m := map[string]string{
			"symbol":           "EURUSD",
			"from":             "2015-01-01",
			"to":               "2025-01-01",
			"adverse-distance": "0.0050",
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
			_, err := config.Load[runConfig](config.Options{
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
// to prove runConfig's own tags wire into that precedence correctly).
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
// overrides produce an identical effective runConfig — and therefore
// an identical Manifest/ConfigDigest downstream, since run.go passes
// these same fields straight through to backtest.NewManifest without
// further transformation.
func TestRunConfig_EquivalentEffectiveConfigFromEitherSource(t *testing.T) {
	fromFile, err := loadRunConfig(t, issue247CandidateYAML, nil)
	require.NoError(t, err)

	fromOverrides, err := config.Load[runConfig](config.Options{
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
