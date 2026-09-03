package backtest

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/num"
)

// runConfig is the typed configuration "trader backtest run" resolves
// via --config (issue #247, EMA-02): the backtest composition inputs
// this command already accepts as individual flags, plus a strategy
// section for the real EMA crossover strategy (strategy/emacross,
// issues #248/#249) run.go constructs whenever --config is given
// (issue #252, EMA-07). config.Load applies its own
// defaults-then-file-then-environment-then-overrides precedence
// (config/doc.go); buildRunConfig below only ever places an explicitly
// Changed flag's value into Overrides, so an unset flag never masks a
// config-file value, matching CONTRIBUTING.org's "explicit CLI
// override > config file > documented defaults" precedence for this
// experiment.
//
// Interval is decoded as a plain string, not restricted by an enum
// tag: parseInterval already normalizes case and reports a clear error
// at the point of use, so duplicating a second, case-sensitive
// validation here would only be able to disagree with it.
//
// Strategy is parsed and validated here; run.go additionally rejects
// any Strategy.Name other than emacross.Name before constructing a
// strategy at all (PR #263 review) — there is no strategy registry, so
// an unsupported or misspelled name must fail loudly rather than
// silently running EMA crossover under a different label. Strategy is
// then passed to Manifest.StrategyParameters via the constructed
// emacross.Strategy's own Config(), so the manifest only ever claims
// parameters that actually governed the run that occurred.
type runConfig struct {
	Backtest backtestSection
	Strategy strategySection
}

// backtestSection mirrors runFlags' own scalar backtest inputs.
// StartingCapital stays a plain string, combined with Currency via
// num.ParseMoney in runBacktest — num.Money's own TextUnmarshaler
// expects its single-field "<amount> <currency>" form (num/encoding.go),
// not the two separate YAML keys #247's own candidate config uses.
type backtestSection struct {
	// Symbol is deliberately not required:"true" here: it is validated
	// in code (buildInstrumentSymbols in run.go), not by config.Load,
	// because a multi-instrument run (repeated --symbol, issue #224)
	// supplies its instruments outside this single-string field
	// entirely — see buildRunConfig's own doc comment.
	Symbol          string    `config:"symbol" flag:"symbol"`
	Interval        string    `config:"interval" flag:"interval" default:"H1"`
	From            string    `config:"from" flag:"from" required:"true"`
	To              string    `config:"to" flag:"to" required:"true"`
	Currency        string    `config:"currency" flag:"currency" default:"USD"`
	StartingCapital string    `config:"starting_capital" flag:"starting-cash" default:"10000"`
	RiskFraction    num.Rate  `config:"risk_fraction" flag:"risk-fraction" default:"0.01"`
	AdverseDistance num.Price `config:"adverse_distance" flag:"adverse-distance" required:"true"`
}

// strategySection is the EMA crossover strategy's own configuration —
// see docs/research/ema-01-experiment-definition.org for the
// crossover/warm-up semantics these periods feed once a real strategy
// consumes them.
// FastPeriod/SlowPeriod default to EMA-01's own reference values
// (docs/research/ema-01-experiment-definition.org) rather than being
// required: an invocation that never mentions the EMA strategy at all
// (today's only real strategy is demoStrategy, which ignores this
// section) must keep working unchanged.
// JSON tags follow the snake_case convention backtest/manifest_test.go
// already establishes for strategy parameters, so a future consumer
// that does marshal this section into Manifest.StrategyParameters
// (EMA-04/EMA-07) produces a manifest consistent with that convention
// rather than Go's default "Name"/"FastPeriod"/"SlowPeriod" keys.
type strategySection struct {
	Name       string `config:"name" flag:"strategy-name" default:"ema-cross" json:"name"`
	FastPeriod int    `config:"fast_period" flag:"fast-period" default:"20" json:"fast_period"`
	SlowPeriod int    `config:"slow_period" flag:"slow-period" default:"50" json:"slow_period"`
}

// Validate implements config's validator hook, checked after every
// source has been applied and every required field is present
// (config/load.go's validateDestination). It covers exactly what plain
// field decoding cannot: relationships between fields.
func (c runConfig) Validate() error {
	if c.Strategy.FastPeriod <= 0 {
		return fmt.Errorf("strategy.fast_period must be positive, got %d", c.Strategy.FastPeriod)
	}
	if c.Strategy.SlowPeriod <= c.Strategy.FastPeriod {
		return fmt.Errorf("strategy.slow_period (%d) must be greater than strategy.fast_period (%d)",
			c.Strategy.SlowPeriod, c.Strategy.FastPeriod)
	}

	from, err := parseDate(c.Backtest.From)
	if err != nil {
		return fmt.Errorf("backtest.from: %w", err)
	}
	to, err := parseDate(c.Backtest.To)
	if err != nil {
		return fmt.Errorf("backtest.to: %w", err)
	}
	if !to.After(from) {
		return fmt.Errorf("backtest.to (%s) must be after backtest.from (%s)", c.Backtest.To, c.Backtest.From)
	}

	if _, err := parseInterval(c.Backtest.Interval); err != nil {
		return fmt.Errorf("backtest.interval: %w", err)
	}

	return nil
}

// buildRunConfig resolves a runConfig from any flags actually Changed
// on cmd, layered under flags.config (if set) and the TRADER_BACKTEST_*/
// TRADER_STRATEGY_* environment variables, via the same config.Load
// every Trader composition root uses (cmd/trader/data/service.go's
// buildDatasetConfig is the identical pattern this mirrors, including
// why only Changed flags are ever placed in Overrides).
//
// --symbol is repeatable (multi-instrument, issue #224) but runConfig's
// own Symbol field is a single string: a config file describes one
// experiment's one instrument, matching #247's own candidate YAML.
// Combining --config with more than one --symbol is rejected outright
// here rather than silently using only the first one.
func buildRunConfig(cmd *cobra.Command, flags runFlags) (runConfig, error) {
	if flags.config != "" && len(flags.symbols) > 1 {
		return runConfig{}, fmt.Errorf("--config describes a single-instrument experiment; " +
			"repeat --symbol without --config for a multi-instrument run")
	}

	overrides := map[string]string{}
	if cmd.Flags().Changed("symbol") && len(flags.symbols) == 1 {
		overrides["symbol"] = flags.symbols[0]
	}
	if cmd.Flags().Changed("interval") {
		overrides["interval"] = flags.interval
	}
	if cmd.Flags().Changed("from") {
		overrides["from"] = flags.from
	}
	if cmd.Flags().Changed("to") {
		overrides["to"] = flags.to
	}
	if cmd.Flags().Changed("currency") {
		overrides["currency"] = flags.currency
	}
	if cmd.Flags().Changed("starting-cash") {
		overrides["starting-cash"] = flags.startingCash
	}
	if cmd.Flags().Changed("risk-fraction") {
		overrides["risk-fraction"] = flags.riskFraction
	}
	if cmd.Flags().Changed("adverse-distance") {
		overrides["adverse-distance"] = flags.adverse
	}
	if cmd.Flags().Changed("strategy-name") {
		overrides["strategy-name"] = flags.strategyName
	}
	if cmd.Flags().Changed("fast-period") {
		overrides["fast-period"] = fmt.Sprintf("%d", flags.fastPeriod)
	}
	if cmd.Flags().Changed("slow-period") {
		overrides["slow-period"] = fmt.Sprintf("%d", flags.slowPeriod)
	}

	return config.Load[runConfig](config.Options{
		EnvPrefix: clictx.EnvPrefix,
		Environ:   os.Environ(),
		FilePath:  flags.config,
		Overrides: overrides,
	})
}
