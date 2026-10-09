package backtestcfg

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/internal/config"
	"github.com/rustyeddy/trader/internal/strategy/emacross"
	"github.com/rustyeddy/trader/num"
)

// RunConfig is the typed configuration "trader backtest run" resolves
// via --config: the backtest composition inputs this command already
// accepts as individual flags, plus a generic strategy selector.
// config.Load applies its own defaults-then-file-then-environment-then-overrides
// precedence; buildRunConfig below only places explicitly changed flags
// into Overrides, so config-file values remain effective by default.
//
// Interval is decoded as a plain string, not restricted by an enum
// tag: parseInterval already normalizes case and reports a clear error
// at the point of use, so duplicating a second, case-sensitive
// validation here would only be able to disagree with it.
//
// Strategy is parsed and validated against the registered in-process
// strategies. An unsupported or misspelled name fails loudly rather than
// silently selecting a different implementation.
//
// Model is the alternative to the backtest experiment inputs for a study
// or scanner (issue #471): it names only the universe, span and account,
// and omits the position-sizing policy (risk_fraction, adverse_distance)
// a model that never trades has no use for. A config uses Model or the
// backtest experiment keys, never needs both; Resolve folds Model into
// Backtest so the rest of the run reads one shape.
type RunConfig struct {
	Backtest BacktestSection
	Strategy StrategySection
	Model    ModelSection
}

// ModelSection is the minimal config for a model (study/scanner) run
// (issue #471). Every field is required once any is set. Symbols is a
// comma-separated universe, like backtest.symbols. Data locations
// (data_raw_root, data_store_root, provider) are infrastructure, not part
// of the experiment, and stay under backtest.
type ModelSection struct {
	Symbols         string `config:"symbols" flag:"model-symbols"`
	Interval        string `config:"interval" flag:"model-interval"`
	From            string `config:"from" flag:"model-from"`
	To              string `config:"to" flag:"model-to"`
	Currency        string `config:"currency" flag:"model-currency"`
	StartingCapital string `config:"starting_capital" flag:"model-starting-cash"`
}

// active reports whether any model key is set.
func (m ModelSection) active() bool { return m != (ModelSection{}) }

// Resolve returns c with a model section folded into Backtest, so
// callers never branch on the config's kind. Explicit backtest.symbol(s),
// interval, from, to, currency and starting_capital values (in practice
// command-line flags, which only ever target backtest keys) win over the
// model's, so `--from` still adjusts a model config. Without a model
// section c is returned unchanged.
func (c RunConfig) Resolve() RunConfig {
	if !c.Model.active() {
		return c
	}
	b := &c.Backtest
	if b.Symbol == "" && b.Symbols == "" {
		b.Symbols = c.Model.Symbols
	}
	if b.From == "" {
		b.From = c.Model.From
	}
	if b.To == "" {
		b.To = c.Model.To
	}
	// Interval, currency and starting capital carry backtest defaults, so
	// "explicit" cannot be told from "default" here; the model's own
	// (required) values apply.
	b.Interval = c.Model.Interval
	b.Currency = c.Model.Currency
	b.StartingCapital = c.Model.StartingCapital
	return c
}

// LoadRunConfig loads a RunConfig with config.Load and resolves a model
// section, the one entry point every composition root should use.
func LoadRunConfig(opts config.Options) (RunConfig, error) {
	cfg, err := config.Load[RunConfig](opts)
	if err != nil {
		return cfg, err
	}
	if !cfg.Model.active() && cfg.Backtest.AdverseDistance.IsZero() {
		// Not in Validate: programmatic callers (tests, the service
		// layer) build RunConfigs without a sizing policy.
		return cfg, &config.Error{Fields: []*config.FieldError{{Path: "backtest.adverse_distance", Err: config.ErrRequired}}}
	}
	return cfg.Resolve(), nil
}

// missing reports a required key absent from the active section, matching
// the loader's own required-field error.
func missing(key string) error {
	return fmt.Errorf("%s: %w", key, config.ErrRequired)
}

// validateModel checks a model config: all six keys present, an external
// strategy to run (an in-process strategy sizes positions from
// risk_fraction/adverse_distance, which a model config does not carry),
// and the sizing keys absent.
func (c RunConfig) validateModel() error {
	m := c.Model
	for _, f := range []struct{ key, val string }{
		{"model.symbols", m.Symbols}, {"model.interval", m.Interval}, {"model.from", m.From},
		{"model.to", m.To}, {"model.currency", m.Currency}, {"model.starting_capital", m.StartingCapital},
	} {
		if f.val == "" {
			return missing(f.key)
		}
	}
	if c.Strategy.Exec == "" {
		return fmt.Errorf("model requires strategy.exec: a model run has no position-sizing policy for an in-process strategy")
	}
	if !c.Backtest.AdverseDistance.IsZero() {
		return fmt.Errorf("backtest.adverse_distance does not apply to a model run; remove it")
	}
	return nil
}

// BacktestSection mirrors runFlags' own scalar backtest inputs.
// StartingCapital stays a plain string, combined with Currency via
// num.ParseMoney in runBacktest — num.Money's own TextUnmarshaler
// expects its single-field "<amount> <currency>" form (num/encoding.go),
// not the two separate YAML keys #247's own candidate config uses.
type BacktestSection struct {
	// Symbol is deliberately not required:"true" here: it is validated
	// in code (buildInstrumentSymbols in run.go), not by config.Load,
	// because a multi-instrument run (repeated --symbol, issue #224)
	// supplies its instruments outside this single-string field
	// entirely — see buildRunConfig's own doc comment.
	Symbol string `config:"symbol" flag:"symbol"`
	// Symbols is a multi-instrument run's universe as a comma-separated
	// list (issue #469), for example "EURUSD,GBPUSD". It is a scalar
	// because the config loader has no list type; explicit --symbol flags
	// override it, and setting it together with Symbol is an error.
	Symbols         string    `config:"symbols" flag:"symbols"`
	Interval        string    `config:"interval" flag:"interval" default:"H1"`
	From            string    `config:"from" flag:"from"`
	To              string    `config:"to" flag:"to"`
	Currency        string    `config:"currency" flag:"currency" default:"USD"`
	StartingCapital string    `config:"starting_capital" flag:"starting-cash" default:"10000"`
	RiskFraction    num.Rate  `config:"risk_fraction" flag:"risk-fraction" default:"0.01"`
	AdverseDistance num.Price `config:"adverse_distance" flag:"adverse-distance"`

	// InitialMarginRatio is the account's initial-margin ratio
	// (ADR-066): the minimum equity required per unit of gross position
	// notional. 1.0 (the default) is unlevered; 0.5 permits 2× gross
	// exposure, 0.25 permits 4×. This one value configures both the
	// account_initial_margin risk rule and the simulated account's
	// fill-time margin model — they are never configured separately.
	InitialMarginRatio num.Rate `config:"initial_margin_ratio" flag:"initial-margin-ratio" default:"1"`

	// DataStoreRoot is the canonical data store's persistent home
	// (issue #268): unlike every other field here, its default is not
	// a research-neutral placeholder but a real, opinionated local
	// path — /srv/trading/data/canonical, chosen to sit next to the
	// raw archive at /srv/trading/data/raw/oanda and kept distinct
	// from the legacy trader-first-try system's own
	// /srv/trading/data/candles cache, which this default must never
	// collide with. An explicit --data-store-root "" (or
	// TRADER_BACKTEST_DATA_STORE_ROOT="") still opts back into a
	// fresh, ephemeral temporary directory per run (runBacktest's own
	// storeRoot resolution) — this is what every test that must not
	// share state across runs relies on, so this default only ever
	// takes effect when nothing more specific overrides it.
	DataStoreRoot string `config:"data_store_root" flag:"data-store-root" default:"/srv/trading/data/canonical"`
	DataRawRoot   string `config:"data_raw_root" flag:"data-raw-root"`
	Provider      string `config:"provider" flag:"provider" default:"oanda"`
}

// StrategySection is the EMA crossover strategy's own configuration —
// Strategy-specific fields live under this generic selector. EMA fields are
// decoded here for the registered ema-cross strategy and ignored by the
// buy-and-hold baseline; future registered strategies can add their own
// construction and validation without changing the top-level config shape.
// JSON tags keep strategy parameters stable in manifests and reports.
type StrategySection struct {
	Name string `config:"name" flag:"strategy-name" default:"buy-and-hold" json:"name"`
	// Exec selects an out-of-tree strategy executable (issue #469),
	// exactly as --strategy-exec does; when set, Name is not checked
	// against the in-process registry. Config is the config-file path
	// forwarded to that executable as StrategyConfigPathEnv, never parsed
	// by Trader (--strategy-config). Neither is a strategy parameter, so
	// neither appears in manifests; an external run records its own
	// provenance in externalStrategyParams.
	Exec       string `config:"exec" flag:"strategy-exec" json:"-"`
	Config     string `config:"config" flag:"strategy-config" json:"-"`
	FastPeriod int    `config:"fast_period" flag:"fast-period" default:"20" json:"fast_period"`
	SlowPeriod int    `config:"slow_period" flag:"slow-period" default:"50" json:"slow_period"`
	// AllowedSide restricts which position direction the strategy may
	// hold (issue #273): "both" (default), "long-only", or
	// "short-only". emacross.Side implements encoding.TextUnmarshaler,
	// so config.Load decodes it directly, the same way num.Rate/
	// num.Price already do.
	AllowedSide emacross.Side `config:"allowed_side" flag:"allowed-side" default:"both" json:"allowed_side"`

	// Quantity switches buy-and-hold into quantity mode (issue #417):
	// buy exactly this quantity, never sized or resized, of the run's
	// single instrument. It is decoded as text so that supplying it —
	// by flag, YAML, or environment — is distinct from omitting it:
	// omitted keeps buy-and-hold's fixed-fraction demo behavior, and a
	// supplied value must be a positive quantity (an explicit 0 is an
	// error, never a silent fallback to the demo). BuyDate and SellDate
	// (YYYY-MM-DD or RFC3339) apply only in quantity mode: buy on the
	// first bar at or after BuyDate (default: backtest.from), and exit
	// on the first bar at or after SellDate if set. The run's end date
	// is not a sell.
	Quantity string `config:"quantity" flag:"quantity" json:"quantity,omitempty"`
	BuyDate  string `config:"buy_date" flag:"buy-date" json:"buy_date,omitempty"`
	SellDate string `config:"sell_date" flag:"sell-date" json:"sell_date,omitempty"`
}

// quantityMode reports whether buy-and-hold runs in quantity mode:
// quantity was supplied, whatever its value (validateBuyHold rejects a
// non-positive one).
func (s StrategySection) quantityMode() bool {
	return s.Name == demoStrategyName && s.Quantity != ""
}

// buyHoldSettings is quantity mode's parsed settings.
type buyHoldSettings struct {
	quantity num.Quantity
	buyDate  *time.Time
	sellDate *time.Time
}

// parseBuyHold parses and validates quantity mode's settings. from is
// the run's start, the effective buy date when buy_date is omitted.
func (s StrategySection) parseBuyHold(from time.Time) (buyHoldSettings, error) {
	var out buyHoldSettings
	q, err := num.ParseQuantity(s.Quantity)
	if err != nil {
		return out, fmt.Errorf("strategy.quantity: %w", err)
	}
	if q.IsZero() {
		return out, fmt.Errorf("strategy.quantity must be positive, got %s", s.Quantity)
	}
	out.quantity = q
	effectiveBuy := from
	if s.BuyDate != "" {
		t, err := parseDate(s.BuyDate)
		if err != nil {
			return out, fmt.Errorf("strategy.buy_date: %w", err)
		}
		out.buyDate, effectiveBuy = &t, t
	}
	if s.SellDate != "" {
		t, err := parseDate(s.SellDate)
		if err != nil {
			return out, fmt.Errorf("strategy.sell_date: %w", err)
		}
		if !t.After(effectiveBuy) {
			return out, fmt.Errorf("strategy.sell_date (%s) must be after the effective buy date (%s)", s.SellDate, effectiveBuy.Format(time.RFC3339))
		}
		out.sellDate = &t
	}
	return out, nil
}

// Validate implements config's validator hook, checked after every
// source has been applied and every required field is present
// (config/load.go's validateDestination). It covers exactly what plain
// field decoding cannot: relationships between fields.
func (c RunConfig) Validate() error {
	if c.Model.active() {
		if err := c.validateModel(); err != nil {
			return err
		}
		c = c.Resolve()
	} else {
		// required:"true" cannot express "required unless a model section
		// supplies them", so the backtest keys are checked here (and
		// backtest.adverse_distance in LoadRunConfig).
		if c.Backtest.From == "" {
			return missing("backtest.from")
		}
		if c.Backtest.To == "" {
			return missing("backtest.to")
		}
	}
	if c.Backtest.Symbol != "" && c.Backtest.Symbols != "" {
		return fmt.Errorf("backtest.symbol and backtest.symbols are mutually exclusive; use symbols for a multi-instrument run")
	}
	if c.Strategy.Config != "" && c.Strategy.Exec == "" {
		return fmt.Errorf("strategy.config requires strategy.exec")
	}
	switch {
	case c.Strategy.Exec != "":
		// An external executable supplies its own strategy; the
		// in-process registry and its parameters do not apply.
	case c.Strategy.Name == demoStrategyName:
		if err := c.Strategy.validateBuyHold(c.Backtest.From); err != nil {
			return err
		}
	case c.Strategy.Name == emacross.Name:
		if c.Strategy.FastPeriod <= 0 {
			return fmt.Errorf("strategy.fast_period must be positive, got %d", c.Strategy.FastPeriod)
		}
		if c.Strategy.SlowPeriod <= c.Strategy.FastPeriod {
			return fmt.Errorf("strategy.slow_period (%d) must be greater than strategy.fast_period (%d)",
				c.Strategy.SlowPeriod, c.Strategy.FastPeriod)
		}
	default:
		return fmt.Errorf("strategy.name %q is not registered; supported strategies are %q and %q",
			c.Strategy.Name, demoStrategyName, emacross.Name)
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

	if c.Backtest.InitialMarginRatio.Sign() <= 0 {
		return fmt.Errorf("backtest.initial_margin_ratio must be positive, got %s", c.Backtest.InitialMarginRatio)
	}

	return nil
}

// validateBuyHold checks buy-and-hold's quantity-mode parameters:
// buy_date and sell_date require quantity, and quantity mode's settings
// must parse (see parseBuyHold). from is backtest.from.
func (s StrategySection) validateBuyHold(from string) error {
	if s.Quantity == "" {
		if s.BuyDate != "" || s.SellDate != "" {
			return fmt.Errorf("strategy.buy_date and strategy.sell_date require strategy.quantity")
		}
		return nil
	}
	start, err := parseDate(from)
	if err != nil {
		return fmt.Errorf("backtest.from: %w", err)
	}
	_, err = s.parseBuyHold(start)
	return err
}
