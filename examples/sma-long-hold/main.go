// Command sma-long-hold is the out-of-tree reference implementation
// issue #383 asks for: a real, non-trivial strategysdk.Strategy
// proving Strategy Protocol v1 (ADR-062) against a strategy that is
// not a "hello world" — SMA/indicator state, a genuine bar-driven
// entry/exit decision, and a ratcheting protective stop — built on
// strategysdk exactly as examples/strategysdk-minimal is, and never
// importing Trader's strategy, backtest, adapters, or any runtime/
// application package.
//
// This intentionally reproduces only strategy/smatrend's own default
// configuration (Config.ExitRuleName == "trailing-stop",
// Config.ReEntryRuleName == "fresh-cross",
// Config.InitialEntryModeName == "fresh-cross" — EQS-01's own
// original, only mechanism, before issue #347/#349 made the exit/
// re-entry/initial-entry rules pluggable), not the full pluggable-
// rule surface: cmd/trader/backtest's own SMA Long Hold equivalence
// test (sma_long_hold_equivalence_test.go) runs this exact scenario
// against strategy/smatrend side by side over identical market data
// and asserts their trades and final account state are identical.
// Reproducing every pluggable ExitRule/ReEntryRule/InitialEntryRule
// variant out-of-tree is out of scope for this issue.
//
// For all three of those defaults, onFlat's entry decision reduces to
// exactly one condition regardless of position history — see
// strategy/smatrend's own doc comment for freshCrossInitialEntryRule/
// freshCrossReEntryRule, both of which return only
// ctx.CrossedAboveSMA — so this implementation needs none of
// smatrend's own re-entry-rule-selection state machine at all: enter
// whenever the close crosses from at-or-below the SMA to strictly
// above it, full stop.
//
// It reuses indicator.SMA directly (a pure analytical package, no
// broker/execution/risk surface) rather than hand-rolling a second
// SMA implementation, since exact bit-for-bit numerical agreement
// with strategy/smatrend's own indicator.SMA use is exactly what the
// equivalence test needs to prove — the issue's constraint is "no
// broker/risk/execution access," not "reimplement shared math from
// scratch."
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategysdk"
)

func main() {
	strat, err := newFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if err := strategysdk.Serve(strat); err != nil {
		log.Fatal(err)
	}
}

// fileConfig is this binary's own config-file shape — read from the
// path cmd/trader/backtest's --strategy-config forwards via the
// TRADER_STRATEGY_CONFIG environment variable (a CLI-owned convention
// documented on that flag, not part of Strategy Protocol v1 itself).
// Field names deliberately echo strategy/smatrend.Config's own JSON
// tags for SMAPeriod/TrailingStopPercent, plus the instrument/interval
// smatrend.New takes as separate constructor arguments rather than
// Config fields.
type fileConfig struct {
	Base                string `json:"base"`
	Quote               string `json:"quote"`
	IntervalUnit        string `json:"interval_unit"`
	IntervalCount       int    `json:"interval_count"`
	SMAPeriod           int    `json:"sma_period"`
	TrailingStopPercent string `json:"trailing_stop_percent"`
}

// defaultConfig matches examples/strategysdk-minimal's own EUR/USD H1
// convention, so this binary is runnable with no config file at all —
// TRADER_STRATEGY_CONFIG, when set, overrides every field.
func defaultConfig() fileConfig {
	return fileConfig{
		Base: "EUR", Quote: "USD",
		IntervalUnit: "hour", IntervalCount: 1,
		SMAPeriod:           20,
		TrailingStopPercent: "0.10",
	}
}

// newFromEnv reads TRADER_STRATEGY_CONFIG (if set) and returns a
// configured *longHold, or an error identifying exactly what in the
// config file was invalid — never a partially-applied default.
func newFromEnv() (*longHold, error) {
	cfg := defaultConfig()
	if path := os.Getenv("TRADER_STRATEGY_CONFIG"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("sma-long-hold: reading TRADER_STRATEGY_CONFIG: %w", err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("sma-long-hold: parsing TRADER_STRATEGY_CONFIG: %w", err)
		}
	}
	return newLongHold(cfg)
}

// longHold is the strategysdk.Strategy implementation: strategy/
// smatrend's own default-configuration behavior (trailing-stop exit,
// fresh-cross entry/re-entry), reduced to the state that behavior
// actually needs. See the package doc comment for why no re-entry-
// rule/initial-entry-rule state machine is needed at all.
type longHold struct {
	inst     instrument.ID
	interval marketdata.Interval

	retainFraction num.Rate // 1 - TrailingStopPercent

	sma   *indicator.SMA
	cross crossState

	// sideLastBar mirrors strategy/smatrend's own identical field:
	// this instrument's position side as observed on the *previous*
	// OnBar call, so the Flat->Long transition (where the trailing
	// stop's own high-water mark resets, matching
	// trailingStopExitRule.OnEntry) is detected exactly once.
	sideLastBar order.PositionSide
	// highWaterMark and lastStop mirror trailingStopExitRule's own
	// identical fields exactly (strategy/smatrend/exitrule.go).
	highWaterMark *num.Price
	lastStop      *num.Price
}

func newLongHold(cfg fileConfig) (*longHold, error) {
	if cfg.SMAPeriod <= 0 {
		return nil, fmt.Errorf("sma-long-hold: sma_period must be positive, got %d", cfg.SMAPeriod)
	}
	base, err := num.ParseCurrency(cfg.Base)
	if err != nil {
		return nil, fmt.Errorf("sma-long-hold: base currency: %w", err)
	}
	quote, err := num.ParseCurrency(cfg.Quote)
	if err != nil {
		return nil, fmt.Errorf("sma-long-hold: quote currency: %w", err)
	}
	// instrument.NewCurrencyPair, not instrument.CurrencyPairID
	// directly (review finding): CurrencyPairID alone would happily
	// construct a semantically invalid instrument for base == quote
	// (for example USD/USD) — NewCurrencyPair rejects that, matching
	// what constructing a real strategy/smatrend in-tree Strategy
	// against the same config would already reject one layer up, at
	// instrument.NewCurrencyPair itself.
	pair, err := instrument.NewCurrencyPair(base, quote)
	if err != nil {
		return nil, fmt.Errorf("sma-long-hold: %w", err)
	}
	unit, err := parseIntervalUnit(cfg.IntervalUnit)
	if err != nil {
		return nil, err
	}
	interval, err := marketdata.NewInterval(unit, cfg.IntervalCount)
	if err != nil {
		return nil, fmt.Errorf("sma-long-hold: interval: %w", err)
	}
	trailingStopPercent, err := num.ParseRate(cfg.TrailingStopPercent)
	if err != nil {
		return nil, fmt.Errorf("sma-long-hold: trailing_stop_percent: %w", err)
	}
	// Mirrors strategy/smatrend.Config.Validate's own bounds for the
	// "trailing-stop" rule exactly (review finding): 0 or negative
	// would place the stop at or above the high-water mark itself
	// (triggering on the very next downtick, or immediately), and 1 or
	// more would place it at or below zero. Parsing alone does not
	// reject any of these, and this reference claims equivalence with
	// smatrend's own default configuration — a value smatrend itself
	// would reject must not silently behave differently out-of-tree.
	one := num.MustParseRate("1")
	if trailingStopPercent.Sign() <= 0 {
		return nil, fmt.Errorf("sma-long-hold: trailing_stop_percent must be positive, got %s", trailingStopPercent)
	}
	if trailingStopPercent.Cmp(one) >= 0 {
		return nil, fmt.Errorf("sma-long-hold: trailing_stop_percent must be less than 1, got %s", trailingStopPercent)
	}
	retain, err := one.Sub(trailingStopPercent)
	if err != nil {
		return nil, fmt.Errorf("sma-long-hold: computing stop retention fraction: %w", err)
	}

	sma, err := indicator.NewSMA(cfg.SMAPeriod)
	if err != nil {
		return nil, fmt.Errorf("sma-long-hold: %w", err)
	}

	return &longHold{
		inst:           pair.ID(),
		interval:       interval,
		retainFraction: retain,
		sma:            sma,
	}, nil
}

func parseIntervalUnit(s string) (marketdata.Unit, error) {
	switch s {
	case "minute":
		return marketdata.UnitMinute, nil
	case "hour":
		return marketdata.UnitHour, nil
	case "day":
		return marketdata.UnitDay, nil
	case "week":
		return marketdata.UnitWeek, nil
	default:
		return 0, fmt.Errorf("sma-long-hold: unrecognized interval_unit %q", s)
	}
}

// Describe implements strategysdk.Strategy. WarmupBars equals the SMA
// period, mirroring strategy/smatrend.Strategy.Describe exactly.
func (s *longHold) Describe() strategysdk.Descriptor {
	return strategysdk.Descriptor{
		Name:    "sma-long-hold",
		Version: "v1",
		Requirements: []strategysdk.DataRequirement{
			{Instrument: s.inst, Interval: s.interval, WarmupBars: s.sma.Period()},
		},
	}
}

// Start implements strategysdk.Strategy.
func (s *longHold) Start(_ context.Context, env strategysdk.Environment) error {
	env.Logger.Info("sma-long-hold starting", "run_id", env.RunID, "start", env.Clock.Now(), "instrument", s.inst, "sma_period", s.sma.Period())
	return nil
}

// OnBar implements strategysdk.Strategy, mirroring strategy/smatrend.
// Strategy.OnBar/onFlat/onLong for exactly the default-rule scenario
// this package's own doc comment describes. Like smatrend itself, it
// never detects its own trailing-stop trigger: by the time OnBar
// observes a bar, the host's own simulator has already advanced the
// broker's resting-order machinery (ADR-026) against that exact bar,
// so side == order.Flat with sideLastBar == order.Long means the stop
// already triggered, not something this function decides itself.
func (s *longHold) OnBar(_ context.Context, event strategysdk.BarEvent, view strategysdk.View) ([]strategysdk.DescribedIntent, []strategysdk.DescribedSignal, error) {
	if !event.Instrument.Equal(s.inst) {
		return nil, nil, nil
	}

	// event.Bar.Close.Float64() is ADR-045's explicit exact-to-
	// analytical conversion boundary: a direct numeric conversion,
	// never a String()/strconv.ParseFloat() round-trip — the same
	// discipline strategy/smatrend.Strategy.OnBar itself follows.
	close := event.Bar.Close.Float64()
	if err := s.sma.Update(close); err != nil {
		return nil, nil, fmt.Errorf("sma-long-hold: updating sma: %w", err)
	}
	if !s.sma.Ready() {
		return nil, nil, nil
	}

	aboveSMA := close > s.sma.Value()
	crossedAbove := s.cross.update(aboveSMA)

	side := currentPositionSide(view, s.inst)

	smaValue := s.sma.Value()

	switch side {
	case order.Flat:
		s.sideLastBar = order.Flat
		if !crossedAbove {
			return nil, nil, nil
		}
		const token = "action"
		in := strategysdk.Enter(s.inst, order.Buy).WithCorrelation(token)
		sig := s.signal(close, smaValue, "enter-long", nil).WithCorrelation(token)
		return []strategysdk.DescribedIntent{in}, []strategysdk.DescribedSignal{sig}, nil

	case order.Long:
		if s.sideLastBar != order.Long {
			// Flat->Long transition: mirrors
			// trailingStopExitRule.OnEntry exactly — reset the
			// high-water mark from this entry bar's own High.
			high := event.Bar.High
			s.highWaterMark = &high
			s.lastStop = nil
		}
		s.sideLastBar = order.Long

		high := event.Bar.High
		if s.highWaterMark == nil || high.Cmp(*s.highWaterMark) > 0 {
			s.highWaterMark = &high
		}
		newStop, err := s.highWaterMark.MulRate(s.retainFraction)
		if err != nil {
			return nil, nil, fmt.Errorf("sma-long-hold: computing trailing stop: %w", err)
		}
		if s.lastStop != nil && newStop.Cmp(*s.lastStop) <= 0 {
			return nil, nil, nil
		}
		s.lastStop = &newStop
		const token = "action"
		in := strategysdk.AdjustStop(s.inst, newStop).WithCorrelation(token)
		sig := s.signal(close, smaValue, "adjust-stop", &newStop).WithCorrelation(token)
		return []strategysdk.DescribedIntent{in}, []strategysdk.DescribedSignal{sig}, nil

	default:
		return nil, nil, fmt.Errorf("sma-long-hold: unexpected position side %v (long-only strategy)", side)
	}
}

// signal builds this bar's decision-evidence record, deliberately
// mirroring strategy/smatrend.Strategy.recordSignal's own Values map
// shape and keys exactly (close/sma/action/exit_rule/reentry_rule/
// phase[/stop_price]) — the two implementations' equivalence test
// (cmd/trader/backtest/sma_long_hold_equivalence_test.go) compares
// Values directly, byte for byte, while deliberately excluding
// Strategy (this binary's own Descriptor.Name, "sma-long-hold",
// intentionally differs from smatrend's "sma-trend" — see that test's
// own doc comment for the full list of intentionally-different,
// documented fields). exit_rule/reentry_rule/phase are fixed literals
// here, not derived state, since this reference implementation only
// ever reproduces smatrend's own default configuration (trailing-stop/
// fresh-cross/fresh-cross, always PhaseFlat — see this package's own
// doc comment).
func (s *longHold) signal(close, smaValue float64, action string, stopPrice *num.Price) strategysdk.DescribedSignal {
	values := map[string]string{
		"close":        strconv.FormatFloat(close, 'f', -1, 64),
		"sma":          strconv.FormatFloat(smaValue, 'f', -1, 64),
		"action":       action,
		"exit_rule":    "trailing-stop",
		"reentry_rule": "fresh-cross",
		"phase":        "flat",
	}
	if stopPrice != nil {
		values["stop_price"] = stopPrice.String()
	}
	return strategysdk.Signal("sma-long-hold", values)
}

// currentPositionSide mirrors strategy/smatrend's own identical
// helper: view's own current side for instID, or order.Flat if no
// open position names it.
func currentPositionSide(view strategysdk.View, instID instrument.ID) order.PositionSide {
	for _, p := range view.Account().Positions {
		if p.Instrument.Equal(instID) {
			return p.Side
		}
	}
	return order.Flat
}

// crossState is strategy/smatrend's own crossState, reproduced here
// field-for-field and behavior-for-behavior (see that package's own
// doc comment for the exact semantics: a signal fires only on a
// genuine cross from at-or-below to strictly above, never merely
// "currently above"). Not shared code with strategy/smatrend — this
// is this binary's own independent half of the same well-known
// algorithm, the same "no shared wire-conversion code" discipline
// strategysdk's own doc comment establishes for the host/guest
// boundary, applied here to strategy logic instead.
type crossState struct {
	have     bool
	aboveSMA bool
}

func (s *crossState) update(currentAbove bool) (crossedAbove bool) {
	if s.have {
		crossedAbove = currentAbove && !s.aboveSMA
	}
	s.aboveSMA = currentAbove
	s.have = true
	return crossedAbove
}
