package backtestcfg

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/internal/config"
	"github.com/rustyeddy/trader/internal/strategy/emacross"
	"github.com/rustyeddy/trader/num"
)

// Run kinds. kind is the top-level discriminator of a run configuration
// document (issue #474, ADR-077): a backtest simulates trading with an
// account and a position-sizing policy; a study runs a trading-free
// strategy executable (a scanner) over market data.
const (
	KindBacktest = "backtest"
	KindStudy    = "study"
)

// RunConfig is the typed configuration "trader backtest run" resolves
// via --config (issue #474, ADR-077). The document separates what kind of
// run it is (Kind), what market data it reads (Market), the mechanics
// specific to that kind (Backtest or Study), and the strategy to run
// (Strategy, for a backtest):
//
//	kind: study                  kind: backtest
//	market: {...}                market: {...}
//	study: {name, exec, config}  backtest: {starting_capital, ...}
//	                             strategy: {...}
//
// config.Load applies its own defaults-then-file-then-environment-then-overrides
// precedence; buildRunConfig only places explicitly changed flags into
// Overrides, so config-file values remain effective by default.
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
// A study carries no account or sizing keys: Resolve supplies the host's
// internal account defaults, so a study never has to state values it does
// not use. Mixing the two kinds' sections is rejected, never guessed at.
type RunConfig struct {
	Kind     string `config:"kind" flag:"kind" default:"backtest" enum:"backtest,study"`
	Market   MarketSection
	Backtest BacktestSection
	Study    StudySection
	Strategy StrategySection
}

// IsStudy reports whether the config is a study run, which trades nothing
// and so has no backtest report worth printing (issue #489).
func (c RunConfig) IsStudy() bool { return c.Kind == KindStudy }

// Internal account defaults a study never states (the host still needs an
// account to run against) and a backtest's optional keys.
const (
	defaultInterval        = "H1"
	defaultCurrency        = "USD"
	defaultStartingCapital = "10000"
)

// Resolve returns c with the effective run settings filled in, so callers
// never branch on the config's kind: a study's launch settings become the
// strategy selection (study.exec -> strategy.exec, study.name ->
// strategy.name), and the account and sizing defaults a kind does not
// state are applied (interval H1, USD, 10000, ...). A zero risk_fraction or initial_margin_ratio is
// treated as unset, since neither has a meaningful zero.
func (c RunConfig) Resolve() RunConfig {
	c.Market.Interval = firstNonEmpty(c.Market.Interval, defaultInterval)
	b := &c.Backtest
	b.Currency = firstNonEmpty(b.Currency, defaultCurrency)
	b.StartingCapital = firstNonEmpty(b.StartingCapital, defaultStartingCapital)
	if c.IsStudy() {
		c.Strategy.Name = c.Study.Name
		c.Strategy.Exec = c.Study.Exec
		c.Strategy.Config = c.Study.Config
	}
	return c
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// LoadRunConfig loads a RunConfig with config.Load: the one entry point
// every composition root should use. Around the generic loader it
//
//   - rejects the pre-#474 grammar with a pointer to its replacement,
//   - routes the existing --strategy-name/-exec/-config overrides to
//     study.name/exec/config when the document is a study, so the CLI surface
//     is the same for both kinds,
//   - rejects any key of the wrong kind that was written in the file, the
//     environment or the overrides, even when it equals the loader default,
//   - and resolves the effective settings.
//
// A study whose study.config is omitted forwards the run file itself to the
// executable, so a scanner can keep its settings in the same document.
func LoadRunConfig(opts config.Options) (RunConfig, error) {
	doc := readDocument(opts)
	if err := rejectLegacyKeys(doc); err != nil {
		return RunConfig{}, err
	}
	opts, err := withIsolation(opts, doc)
	if err != nil {
		return RunConfig{}, err
	}
	kind := effectiveKind(opts, doc)
	if kind == KindStudy {
		opts.Overrides = routeStudyOverrides(opts.Overrides)
	}
	cfg, err := config.Load[RunConfig](opts)
	if err != nil {
		return cfg, err
	}
	if err := checkSections(kind, presentKeys(opts, doc)); err != nil {
		return cfg, err
	}
	cfg.Market.DataRawRoot = fileRelative(opts, doc, "market", "data_raw_root", "data-raw-root", cfg.Market.DataRawRoot)
	cfg.Market.DataStoreRoot = fileRelative(opts, doc, "market", "data_store_root", "data-store-root", cfg.Market.DataStoreRoot)
	if cfg.IsStudy() && cfg.Study.Config == "" {
		cfg.Study.Config = opts.FilePath
	}
	if !cfg.IsStudy() && cfg.Backtest.AdverseDistance.IsZero() {
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

// MarketSection answers "what market data does this run operate on?"
// (issue #474). It is shared by every kind, so a backtest and a study never
// keep parallel copies of the universe, span or data locations. Symbols is
// a comma-separated universe, a scalar because the config loader has no
// list type; explicit --symbol flags replace it.
type MarketSection struct {
	Symbols  string `config:"symbols" flag:"symbols"`
	Interval string `config:"interval" flag:"interval"`
	From     string `config:"from" flag:"from"`
	To       string `config:"to" flag:"to"`
	Provider string `config:"provider" flag:"provider" default:"oanda"`

	// DataStoreRoot is the canonical data store's persistent home
	// (issue #268): unlike every other field here, its default is not
	// a research-neutral placeholder but a real, opinionated local
	// path — /srv/trading/data/canonical, chosen to sit next to the
	// raw archive at /srv/trading/data/raw/oanda and kept distinct
	// from the legacy trader-first-try system's own
	// /srv/trading/data/candles cache, which this default must never
	// collide with. An explicit --data-store-root "" (or
	// TRADER_MARKET_DATA_STORE_ROOT="") still opts back into a
	// fresh, ephemeral temporary directory per run (runBacktest's own
	// storeRoot resolution) — this is what every test that must not
	// share state across runs relies on, so this default only ever
	// takes effect when nothing more specific overrides it.
	DataStoreRoot string `config:"data_store_root" flag:"data-store-root" default:"/srv/trading/data/canonical"`
	DataRawRoot   string `config:"data_raw_root" flag:"data-raw-root"`
}

// BacktestSection is the simulation's account and sizing mechanics: the
// settings only a backtest has (issue #474). A study that sets any of them
// to something other than the host's internal default (USD, 10000, 0.01,
// 1; adverse_distance has none) is an ambiguous mixed document and is
// rejected. The defaults are exempt because Resolve fills them in for a
// study, and run validates the resolved config. StartingCapital stays a plain string, combined
// with Currency via num.ParseMoney in runBacktest — num.Money's own
// TextUnmarshaler expects its single-field "<amount> <currency>" form
// (num/encoding.go), not the two separate YAML keys #247's candidate config
// uses.
type BacktestSection struct {
	Currency        string    `config:"currency" flag:"currency"`
	StartingCapital string    `config:"starting_capital" flag:"starting-cash"`
	RiskFraction    num.Rate  `config:"risk_fraction" flag:"risk-fraction" default:"0.01"`
	AdverseDistance num.Price `config:"adverse_distance" flag:"adverse-distance"`

	// InitialMarginRatio is the account's initial-margin ratio
	// (ADR-066): the minimum equity required per unit of gross position
	// notional. 1.0 (the default) is unlevered; 0.5 permits 2× gross
	// exposure, 0.25 permits 4×. This one value configures both the
	// account_initial_margin risk rule and the simulated account's
	// fill-time margin model — they are never configured separately.
	InitialMarginRatio num.Rate `config:"initial_margin_ratio" flag:"initial-margin-ratio" default:"1"`
}

// StudySection is the launch identity of a study (issue #474): the
// out-of-tree executable to run, never its parameters. Trader does not
// parse those; they stay owned by the executable. Config is the path
// forwarded to it as StrategyConfigPathEnv and defaults to the run file
// itself, so a scanner may keep its settings in its own section of the same
// document.
type StudySection struct {
	Name   string `config:"name" flag:"study-name"`
	Exec   string `config:"exec" flag:"study-exec"`
	Config string `config:"config" flag:"study-config"`
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
// field decoding cannot: relationships between fields, and the rule that a
// document is exactly one kind (issue #474): a section belonging to the
// other kind is an error, never silently ignored.
func (c RunConfig) Validate() error {
	switch c.Kind {
	case KindStudy:
		if err := c.validateStudy(); err != nil {
			return err
		}
	case KindBacktest, "": // empty: a programmatically built RunConfig, a backtest
	default:
		return fmt.Errorf("kind %q is not supported; use %q or %q", c.Kind, KindBacktest, KindStudy)
	}
	if c.Market.From == "" {
		return missing("market.from")
	}
	if c.Market.To == "" {
		return missing("market.to")
	}
	c = c.Resolve()
	if c.Strategy.Config != "" && c.Strategy.Exec == "" {
		return fmt.Errorf("strategy.config requires strategy.exec")
	}
	switch {
	case c.Strategy.Exec != "":
		// An external executable supplies its own strategy; the
		// in-process registry and its parameters do not apply.
	case c.Strategy.Name == demoStrategyName:
		if err := c.Strategy.validateBuyHold(c.Market.From); err != nil {
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

	from, err := parseDate(c.Market.From)
	if err != nil {
		return fmt.Errorf("market.from: %w", err)
	}
	to, err := parseDate(c.Market.To)
	if err != nil {
		return fmt.Errorf("market.to: %w", err)
	}
	if !to.After(from) {
		return fmt.Errorf("market.to (%s) must be after market.from (%s)", c.Market.To, c.Market.From)
	}

	if _, err := parseInterval(c.Market.Interval); err != nil {
		return fmt.Errorf("market.interval: %w", err)
	}

	if c.Backtest.InitialMarginRatio.Sign() <= 0 {
		return fmt.Errorf("backtest.initial_margin_ratio must be positive, got %s", c.Backtest.InitialMarginRatio)
	}

	return nil
}

// validateStudy checks the values a kind: study document must carry. Keys
// belonging to the other kind are rejected by checkSections, which sees which
// keys were actually written; Validate only sees effective values, in which
// an explicit default is indistinguishable from an absent key.
func (c RunConfig) validateStudy() error {
	if c.Study.Name == "" {
		return missing("study.name")
	}
	if c.Study.Exec == "" {
		return missing("study.exec")
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
