package smatrend

import (
	"fmt"

	"github.com/rustyeddy/trader/num"
)

// DefaultExitRuleName, DefaultReEntryRuleName, and
// DefaultInitialEntryModeName are the rule/mode names Config resolves
// to when their corresponding Config field is left empty — EQS-01's
// own original, only mechanisms (issue #335), so every existing
// caller that predates issue #347/#349's pluggable rules keeps
// identical behavior without editing a single Config literal.
const (
	DefaultExitRuleName    = "trailing-stop"
	DefaultReEntryRuleName = "fresh-cross"
)

// Config is this strategy's own typed configuration — never a generic
// map[string]any (architecture document's own "parameters should be
// strongly typed inside a strategy" rule).
//
// JSON tags follow the snake_case convention strategy/emacross.Config
// already establishes for strategy parameters: a composition root can
// pass Config straight through as backtest.ManifestParams.
// StrategyParameters, which NewManifest canonically marshals via
// json.Marshal, and get "sma_period"/"trailing_stop_percent" keys
// rather than Go's default "SMAPeriod"/"TrailingStopPercent".
type Config struct {
	// SMAPeriod is the simple moving average's lookback, in bars — 200
	// for EQS-01's own reference configuration.
	SMAPeriod int `json:"sma_period"`
	// TrailingStopPercent is the fraction of the high-water mark given
	// back before the trailing stop triggers — 0.10 ("10%") for
	// EQS-01's own reference configuration, meaning the stop rests at
	// 90% of the high-water mark. Meaningful when ExitRuleName is
	// "trailing-stop" (the default) or "probation-trend" (issue #349,
	// governing that rule's TRENDING-phase stop identically); ignored
	// by "sma-cross". Must be strictly between 0 and 1 whenever it is
	// meaningful: zero would place the stop exactly at the high-water
	// mark itself (triggering on the very next downtick), and 1 or
	// more would place it at or below zero.
	TrailingStopPercent num.Rate `json:"trailing_stop_percent"`
	// ExitRuleName selects which ExitRule governs the protective exit
	// while long (issue #347). Empty defaults to DefaultExitRuleName.
	// See exitRuleRegistry for the full set of recognized names.
	ExitRuleName string `json:"exit_rule"`
	// ReEntryRuleName selects which ReEntryRule governs re-entry after
	// a stop-out (issue #347) — never the very first entry, which is
	// instead governed by InitialEntryModeName; see ReEntryRule's own
	// doc comment. Empty defaults to DefaultReEntryRuleName. See
	// reEntryRuleRegistry for the full set of recognized names.
	ReEntryRuleName string `json:"reentry_rule"`
	// InitialEntryModeName selects which InitialEntryRule governs this
	// strategy's very first-ever entry, before it has ever held or
	// exited a position (issue #349 review). Empty defaults to
	// DefaultInitialEntryModeName. See initialEntryRuleRegistry for
	// the full set of recognized names.
	InitialEntryModeName string `json:"initial_entry_mode"`
	// InitialStopBelowSMA is the fraction below the SMA the
	// "probation-trend" ExitRule's PROBATION-phase protective stop
	// rests at — 0.01 ("1%") in the SMA Long Hold playbook's own
	// reference configuration. Only meaningful when ExitRuleName is
	// "probation-trend"; ignored otherwise. Must be strictly between
	// 0 and 1 whenever it is meaningful, for the same reason
	// TrailingStopPercent must be.
	InitialStopBelowSMA num.Rate `json:"initial_stop_below_sma"`
	// TrailActivationGain is the fractional gain from entry (real
	// average fill price) at which the "probation-trend" ExitRule
	// transitions from PROBATION to TRENDING and switches from its
	// tight SMA-relative stop to the ordinary high-water-mark trailing
	// stop — 0.05 ("5%") in the playbook's own reference
	// configuration. Only meaningful when ExitRuleName is
	// "probation-trend"; ignored otherwise. Must be strictly positive:
	// zero or negative would activate on entry itself, collapsing
	// PROBATION to no real protection at all.
	TrailActivationGain num.Rate `json:"trail_activation_gain"`
}

// exitRuleName returns ExitRuleName, or DefaultExitRuleName when it is
// empty.
func (c Config) exitRuleName() string {
	if c.ExitRuleName == "" {
		return DefaultExitRuleName
	}
	return c.ExitRuleName
}

// reEntryRuleName returns ReEntryRuleName, or DefaultReEntryRuleName
// when it is empty.
func (c Config) reEntryRuleName() string {
	if c.ReEntryRuleName == "" {
		return DefaultReEntryRuleName
	}
	return c.ReEntryRuleName
}

// initialEntryModeName returns InitialEntryModeName, or
// DefaultInitialEntryModeName when it is empty.
func (c Config) initialEntryModeName() string {
	if c.InitialEntryModeName == "" {
		return DefaultInitialEntryModeName
	}
	return c.InitialEntryModeName
}

// StopFraction returns the fraction of the high-water mark the
// trailing stop retains — 1 - TrailingStopPercent (0.90 for EQS-01's
// own 10% reference configuration) — the value both
// trailingStopExitRule and probationTrendExitRule's own TRENDING
// phase multiply the high-water mark by to compute the stop price.
// Never called by any other ExitRule.
func (c Config) StopFraction() (num.Rate, error) {
	one := num.MustParseRate("1")
	retain, err := one.Sub(c.TrailingStopPercent)
	if err != nil {
		return num.Rate{}, fmt.Errorf("smatrend: computing stop retention fraction: %w", err)
	}
	return retain, nil
}

// Validate reports whether c is well-formed: SMAPeriod positive,
// ExitRuleName/ReEntryRuleName/InitialEntryModeName (or their
// defaults) recognized, TrailingStopPercent strictly between 0 and 1
// whenever the selected ExitRule is "trailing-stop" or
// "probation-trend", and — only when the selected ExitRule is
// "probation-trend" — InitialStopBelowSMA strictly between 0 and 1
// and TrailActivationGain strictly positive.
func (c Config) Validate() error {
	if c.SMAPeriod <= 0 {
		return fmt.Errorf("smatrend: sma period must be positive, got %d", c.SMAPeriod)
	}
	if _, ok := exitRuleRegistry[c.exitRuleName()]; !ok {
		return fmt.Errorf("smatrend: unknown exit_rule %q", c.exitRuleName())
	}
	if _, ok := reEntryRuleRegistry[c.reEntryRuleName()]; !ok {
		return fmt.Errorf("smatrend: unknown reentry_rule %q", c.reEntryRuleName())
	}
	if _, ok := initialEntryRuleRegistry[c.initialEntryModeName()]; !ok {
		return fmt.Errorf("smatrend: unknown initial_entry_mode %q", c.initialEntryModeName())
	}

	one := num.MustParseRate("1")

	switch c.exitRuleName() {
	case "trailing-stop", "probation-trend":
		if c.TrailingStopPercent.Sign() <= 0 {
			return fmt.Errorf("smatrend: trailing stop percent must be positive, got %s", c.TrailingStopPercent)
		}
		if c.TrailingStopPercent.Cmp(one) >= 0 {
			return fmt.Errorf("smatrend: trailing stop percent must be less than 1, got %s", c.TrailingStopPercent)
		}
	}

	if c.exitRuleName() == "probation-trend" {
		if c.InitialStopBelowSMA.Sign() <= 0 {
			return fmt.Errorf("smatrend: initial stop below sma must be positive, got %s", c.InitialStopBelowSMA)
		}
		if c.InitialStopBelowSMA.Cmp(one) >= 0 {
			return fmt.Errorf("smatrend: initial stop below sma must be less than 1, got %s", c.InitialStopBelowSMA)
		}
		if c.TrailActivationGain.Sign() <= 0 {
			return fmt.Errorf("smatrend: trail activation gain must be positive, got %s", c.TrailActivationGain)
		}
	}

	return nil
}
