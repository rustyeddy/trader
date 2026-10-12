package backtestcfg

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/config"
	"github.com/rustyeddy/trader/internal/strategy/emacross"
	"github.com/rustyeddy/trader/num"
)

// issue247CandidateYAML is #247's candidate YAML in the kind/market
// grammar of issue #474 (the market keys moved out of backtest:).
const issue247CandidateYAML = `
kind: backtest

market:
  symbols: EURUSD
  interval: H1
  from: 2015-01-01
  to: 2025-01-01

backtest:
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
	cfg, err := config.Load[RunConfig](config.Options{
		Environ:     []string{},
		FileContent: []byte(yaml),
		Overrides:   overrides,
	})
	return cfg.Resolve(), err // defaults are applied by Resolve (LoadRunConfig does the same)
}

func TestRunConfig_ParsesIssue247CandidateYAML(t *testing.T) {
	cfg, err := loadRunConfig(t, issue247CandidateYAML, nil)
	require.NoError(t, err)

	assert.Equal(t, KindBacktest, cfg.Kind)
	assert.Equal(t, "EURUSD", cfg.Market.Symbols)
	assert.Equal(t, "H1", cfg.Market.Interval)
	assert.Equal(t, "2015-01-01", cfg.Market.From)
	assert.Equal(t, "2025-01-01", cfg.Market.To)
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
market:
  symbols: EURUSD
  from: 2015-01-01
  to: 2025-01-01
backtest:
  adverse_distance: 0.0050
`
	cfg, err := loadRunConfig(t, minimal, nil)
	require.NoError(t, err)

	assert.Equal(t, KindBacktest, cfg.Kind, "kind defaults to backtest")
	assert.Equal(t, "H1", cfg.Market.Interval)
	assert.Equal(t, "oanda", cfg.Market.Provider)
	assert.Equal(t, "USD", cfg.Backtest.Currency)
	assert.Equal(t, "10000", cfg.Backtest.StartingCapital)
	assert.Equal(t, "0.01", cfg.Backtest.RiskFraction.String())
	assert.Equal(t, "1", cfg.Backtest.InitialMarginRatio.String())
	assert.Equal(t, "buy-and-hold", cfg.Strategy.Name)
	assert.Equal(t, 20, cfg.Strategy.FastPeriod)
	assert.Equal(t, 50, cfg.Strategy.SlowPeriod)
	assert.Equal(t, "/srv/trading/data/canonical", cfg.Market.DataStoreRoot,
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
market:
  symbols: EURUSD
  from: 2015-01-01
  to: 2025-01-01
backtest:
  adverse_distance: 0.0050
`
	cfg, err := loadRunConfig(t, minimal, map[string]string{"data-store-root": ""})
	require.NoError(t, err)
	assert.Empty(t, cfg.Market.DataStoreRoot)
}

func TestRunConfig_MissingRequiredFieldFails(t *testing.T) {
	const missingFrom = `
market:
  symbols: EURUSD
  to: 2025-01-01
backtest:
  adverse_distance: 0.0050
`
	_, err := loadRunConfig(t, missingFrom, nil)
	require.Error(t, err)
	// Checked in RunConfig.Validate, so the sentinel is flattened into the
	// validation message.
	assert.ErrorIs(t, err, config.ErrValidation)
	assert.ErrorContains(t, err, "market.from")
	assert.ErrorContains(t, err, config.ErrRequired.Error())
}

func TestRunConfig_ValidateRejectsInvalidRelationships(t *testing.T) {
	base := func(overrides map[string]string) map[string]string {
		m := map[string]string{
			"symbols":          "EURUSD",
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
			wantErr:   "market.to",
		},
		{
			name:      "invalid interval",
			overrides: base(map[string]string{"interval": "H2"}),
			wantErr:   "market.interval",
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

// TestRunConfig_EnvironmentSitsBetweenFileAndOverrides pins the documented
// precedence for the new sections (issue #474): file < environment <
// overrides, with market keys under TRADER_MARKET_*.
func TestRunConfig_EnvironmentSitsBetweenFileAndOverrides(t *testing.T) {
	load := func(overrides map[string]string) RunConfig {
		cfg, err := config.Load[RunConfig](config.Options{
			EnvPrefix:   "TRADER",
			Environ:     []string{"TRADER_MARKET_FROM=2016-01-01", "TRADER_MARKET_INTERVAL=D1"},
			FileContent: []byte(issue247CandidateYAML),
			Overrides:   overrides,
		})
		require.NoError(t, err)
		return cfg
	}

	cfg := load(nil)
	assert.Equal(t, "2016-01-01", cfg.Market.From, "environment beats the file")
	assert.Equal(t, "D1", cfg.Market.Interval)

	cfg = load(map[string]string{"from": "2017-01-01"})
	assert.Equal(t, "2017-01-01", cfg.Market.From, "an override beats the environment")
	assert.Equal(t, "D1", cfg.Market.Interval)
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
			"symbols":          "EURUSD",
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

	assert.Equal(t, fromFile, fromOverrides.Resolve())
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
		assert.Equal(t, "EURUSD,GBPUSD", cfg.Market.Symbols)
	})
	t.Run("an unregistered name still fails without exec", func(t *testing.T) {
		_, err := load(map[string]string{"strategy-name": "some-external-name"})
		require.ErrorContains(t, err, "not registered")
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
	cfg.Market.From, cfg.Market.To = "2024-01-01", "2024-02-01"
	cfg.Market.Interval = "D1"
	cfg.Backtest.InitialMarginRatio = num.MustParseRate("1")
	cfg.Strategy.Exec = "./scanner"
	_, err := run(context.Background(), Request{Config: cfg, Symbols: []string{"EURUSD"}})
	require.ErrorIs(t, err, ErrInvalidRun)
	require.ErrorContains(t, err, "no ExternalStrategy")
}

// studyYAML is the study grammar of issue #474: a market, and the launch
// identity of the executable; no account, sizing, or strategy keys.
const studyYAML = `
kind: study

market:
  symbols: >-
    EURUSD, GBPUSD,
    USDJPY
  interval: D1
  provider: oanda
  from: 2020-01-01T00:00:00Z
  to: 2024-12-31T00:00:00Z

study:
  name: forex-atr
  exec: ./scanner
`

func loadStudy(t *testing.T, yaml string, overrides map[string]string) (RunConfig, error) {
	t.Helper()
	return LoadRunConfig(config.Options{Environ: []string{}, FileContent: []byte(yaml), Overrides: overrides})
}

// TestStudyConfig covers issue #474: a study states only its market and
// launch identity, and resolves onto the host's strategy selection.
func TestStudyConfig(t *testing.T) {
	t.Run("minimal study needs no account or sizing keys", func(t *testing.T) {
		cfg, err := loadStudy(t, studyYAML, nil)
		require.NoError(t, err)
		assert.True(t, cfg.IsStudy())
		assert.Contains(t, cfg.Market.Symbols, "GBPUSD")
		assert.Equal(t, "D1", cfg.Market.Interval)
		assert.Equal(t, "2020-01-01T00:00:00Z", cfg.Market.From)
		assert.Equal(t, "2024-12-31T00:00:00Z", cfg.Market.To)
	})
	t.Run("the host's internal account defaults are applied, not required", func(t *testing.T) {
		cfg, err := loadStudy(t, studyYAML, nil)
		require.NoError(t, err)
		assert.Equal(t, "USD", cfg.Backtest.Currency)
		assert.Equal(t, "10000", cfg.Backtest.StartingCapital)
		assert.Equal(t, "1", cfg.Backtest.InitialMarginRatio.String())
	})
	t.Run("study.exec and study.name become the strategy selection", func(t *testing.T) {
		cfg, err := loadStudy(t, studyYAML, nil)
		require.NoError(t, err)
		assert.Equal(t, "./scanner", cfg.Strategy.Exec)
		assert.Equal(t, "forex-atr", cfg.Strategy.Name)
	})
	t.Run("study.config defaults to the run file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "study.yml")
		require.NoError(t, os.WriteFile(path, []byte(studyYAML), 0o600))
		cfg, err := LoadRunConfig(config.Options{Environ: []string{}, FilePath: path})
		require.NoError(t, err)
		assert.Equal(t, path, cfg.Strategy.Config)
	})
	t.Run("an explicit study.config wins over the run file", func(t *testing.T) {
		cfg, err := loadStudy(t, studyYAML+"  config: other.yml\n", nil)
		require.NoError(t, err)
		assert.Equal(t, "other.yml", cfg.Strategy.Config)
	})
	t.Run("the existing strategy flags override study.exec, study.name and study.config", func(t *testing.T) {
		cfg, err := loadStudy(t, studyYAML+"  config: file.yml\n", map[string]string{
			"strategy-exec": "./flag-scanner", "strategy-name": "flag-name", "strategy-config": "flag.yml",
		})
		require.NoError(t, err)
		assert.Equal(t, "./flag-scanner", cfg.Strategy.Exec)
		assert.Equal(t, "flag-name", cfg.Strategy.Name)
		assert.Equal(t, "flag.yml", cfg.Strategy.Config)
		assert.Equal(t, "./flag-scanner", cfg.Study.Exec)
	})
	t.Run("a study launch identity can come from flags alone", func(t *testing.T) {
		const noStudy = "kind: study\nmarket:\n  symbols: EURUSD\n  from: 2024-01-01\n  to: 2024-02-01\n"
		cfg, err := loadStudy(t, noStudy, map[string]string{"strategy-exec": "./s", "strategy-name": "n"})
		require.NoError(t, err)
		assert.Equal(t, "./s", cfg.Strategy.Exec)
	})
	t.Run("flags still override market values", func(t *testing.T) {
		cfg, err := loadStudy(t, studyYAML, map[string]string{"from": "2021-01-01", "symbols": "EURUSD"})
		require.NoError(t, err)
		assert.Equal(t, "2021-01-01", cfg.Market.From)
		assert.Equal(t, "EURUSD", cfg.Market.Symbols)
	})
	t.Run("an invalid explicit interval is reported", func(t *testing.T) {
		_, err := loadStudy(t, studyYAML, map[string]string{"interval": "bogus"})
		require.ErrorContains(t, err, "market.interval")
	})
	t.Run("market.from and market.to are required", func(t *testing.T) {
		for _, key := range []string{"from", "to"} {
			var kept []string
			for _, line := range strings.Split(studyYAML, "\n") {
				if strings.HasPrefix(line, "  "+key+":") {
					continue
				}
				kept = append(kept, line)
			}
			_, err := loadStudy(t, strings.Join(kept, "\n"), nil)
			require.Error(t, err, key)
			assert.ErrorContains(t, err, "market."+key)
		}
	})
	t.Run("study.name and study.exec are required", func(t *testing.T) {
		_, err := loadStudy(t, strings.Replace(studyYAML, "  name: forex-atr\n", "", 1), nil)
		require.ErrorContains(t, err, "study.name")
		_, err = loadStudy(t, strings.Replace(studyYAML, "  exec: ./scanner\n", "", 1), nil)
		require.ErrorContains(t, err, "study.exec")
	})
}

// TestKindRejectsMixedDocuments covers issue #474's "reject ambiguous
// combinations rather than silently choosing one".
func TestKindRejectsMixedDocuments(t *testing.T) {
	t.Run("backtest keys are rejected beside kind: study, even when equal to a default", func(t *testing.T) {
		for _, key := range []string{
			"currency: USD", "currency: EUR", "starting_capital: 10000", "starting_capital: 100",
			"risk_fraction: 0.01", "risk_fraction: 0.02", "adverse_distance: 0.01",
			"initial_margin_ratio: 1", "initial_margin_ratio: 0.5",
		} {
			_, err := loadStudy(t, studyYAML+"backtest:\n  "+key+"\n", nil)
			require.ErrorContains(t, err, "does not apply to kind: study", key)
			assert.ErrorContains(t, err, "backtest."+strings.SplitN(key, ":", 2)[0], key)
		}
	})
	t.Run("strategy-only keys are rejected beside kind: study", func(t *testing.T) {
		for _, key := range []string{
			"name: ema-cross", "name: buy-and-hold", "fast_period: 5", "fast_period: 20",
			"slow_period: 60", "allowed_side: both", "quantity: 5", "buy_date: 2024-01-02", "sell_date: 2024-01-03",
		} {
			_, err := loadStudy(t, studyYAML+"strategy:\n  "+key+"\n", nil)
			require.ErrorContains(t, err, "does not apply to kind: study", key)
			assert.ErrorContains(t, err, "strategy."+strings.SplitN(key, ":", 2)[0], key)
		}
	})
	t.Run("strategy.exec and strategy.config point at their study replacements", func(t *testing.T) {
		_, err := loadStudy(t, studyYAML+"strategy:\n  exec: ./other\n", nil)
		require.ErrorContains(t, err, "strategy.exec does not apply to kind: study; use study.exec")
		_, err = loadStudy(t, studyYAML+"strategy:\n  config: x.yml\n", nil)
		require.ErrorContains(t, err, "strategy.config does not apply to kind: study; use study.config")
	})
	t.Run("a wrong-kind key is rejected whichever source wrote it", func(t *testing.T) {
		env := func(kv ...string) config.Options {
			return config.Options{EnvPrefix: "TRADER", Environ: kv, FileContent: []byte(studyYAML)}
		}
		_, err := LoadRunConfig(env("TRADER_BACKTEST_CURRENCY=USD"))
		require.ErrorContains(t, err, "backtest.currency does not apply to kind: study")
		_, err = LoadRunConfig(env("TRADER_STRATEGY_EXEC=./other"))
		require.ErrorContains(t, err, "strategy.exec does not apply to kind: study")
		_, err = LoadRunConfig(config.Options{FileContent: []byte(studyYAML), Overrides: map[string]string{"adverse-distance": "0.01"}})
		require.ErrorContains(t, err, "backtest.adverse_distance does not apply to kind: study")
		_, err = LoadRunConfig(config.Options{FileContent: []byte(studyYAML), Overrides: map[string]string{"fast-period": "20"}})
		require.ErrorContains(t, err, "strategy.fast_period does not apply to kind: study")
	})
	t.Run("a study key in the environment is rejected beside kind: backtest", func(t *testing.T) {
		_, err := LoadRunConfig(config.Options{
			EnvPrefix: "TRADER", Environ: []string{"TRADER_STUDY_EXEC=./x"}, FileContent: []byte(issue247CandidateYAML),
		})
		require.ErrorContains(t, err, "study.exec requires kind: study")
	})
	t.Run("a study section requires kind: study", func(t *testing.T) {
		doc := strings.Replace(studyYAML, "kind: study\n", "kind: backtest\n", 1) + "backtest:\n  adverse_distance: 0.01\n"
		_, err := loadStudy(t, doc, nil)
		require.ErrorContains(t, err, "requires kind: study")
		// Omitting kind defaults to backtest, so the same document is rejected too.
		_, err = loadStudy(t, strings.Replace(studyYAML, "kind: study\n", "", 1)+"backtest:\n  adverse_distance: 0.01\n", nil)
		require.ErrorContains(t, err, "study.name requires kind: study")
	})
	t.Run("an unknown kind is rejected", func(t *testing.T) {
		_, err := loadStudy(t, strings.Replace(studyYAML, "kind: study", "kind: live", 1), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "kind")
	})
	t.Run("a backtest still requires adverse_distance", func(t *testing.T) {
		_, err := loadStudy(t, "market:\n  symbols: EURUSD\n  from: 2024-01-01\n  to: 2024-02-01\n", nil)
		require.ErrorIs(t, err, config.ErrRequired)
		assert.ErrorContains(t, err, "backtest.adverse_distance")
	})
}

// TestLegacyGrammarIsRejectedWithItsReplacement covers issue #474's
// migration rule: the pre-#474 shapes are not accepted, and the error says
// where each key went.
func TestLegacyGrammarIsRejectedWithItsReplacement(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"model section": {
			"model:\n  symbols: EURUSD\n  from: 2024-01-01\n  to: 2024-02-01\n", "kind: study",
		},
		"backtest.symbol": {
			"backtest:\n  symbol: EURUSD\n  adverse_distance: 0.01\n", "backtest.symbol moved to market.symbols",
		},
		"backtest.symbols": {
			"backtest:\n  symbols: EURUSD\n  adverse_distance: 0.01\n", "backtest.symbols moved to market.symbols",
		},
		"backtest.from": {
			"backtest:\n  from: 2024-01-01\n  adverse_distance: 0.01\n", "backtest.from moved to market.from",
		},
		"backtest.data_raw_root": {
			"backtest:\n  data_raw_root: /x\n  adverse_distance: 0.01\n", "backtest.data_raw_root moved to market.data_raw_root",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadStudy(t, tc.doc, nil)
			require.ErrorContains(t, err, tc.want)
			assert.ErrorContains(t, err, "ADR-077")
		})
	}
	t.Run("a scanner's own sections are not mistaken for legacy keys", func(t *testing.T) {
		_, err := loadStudy(t, studyYAML+"scanner:\n  atr_periods: [5, 10]\n  from: ignored\n", nil)
		require.NoError(t, err)
	})
}

func TestIsStudy(t *testing.T) {
	cfg, err := loadStudy(t, studyYAML, nil)
	require.NoError(t, err)
	assert.True(t, cfg.IsStudy(), "Resolve keeps kind, so a resolved config is still a study")

	bt, err := loadRunConfig(t, issue247CandidateYAML, nil)
	require.NoError(t, err)
	assert.False(t, bt.IsStudy())
}
