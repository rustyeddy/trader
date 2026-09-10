package smatrend

import (
	"fmt"

	"github.com/rustyeddy/trader/num"
)

// DefaultExitRuleName and DefaultReEntryRuleName are the rule names
// Config resolves to when ExitRuleName/ReEntryRuleName are left empty
// — EQS-01's own original, only mechanisms (issue #335), so every
// existing caller that predates issue #347's pluggable rules keeps
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
	// 90% of the high-water mark. Only meaningful when ExitRuleName is
	// "trailing-stop" (the default); ignored by any other ExitRule.
	// Must be strictly between 0 and 1 whenever it is meaningful: zero
	// would place the stop exactly at the high-water mark itself
	// (triggering on the very next downtick), and 1 or more would
	// place it at or below zero.
	TrailingStopPercent num.Rate `json:"trailing_stop_percent"`
	// ExitRuleName selects which ExitRule governs the protective exit
	// while long (issue #347). Empty defaults to DefaultExitRuleName.
	// See exitRuleRegistry for the full set of recognized names.
	ExitRuleName string `json:"exit_rule"`
	// ReEntryRuleName selects which ReEntryRule governs re-entry after
	// a stop-out (issue #347) — never the very first entry, which
	// always uses a fresh cross above the SMA directly; see
	// ReEntryRule's own doc comment. Empty defaults to
	// DefaultReEntryRuleName. See reEntryRuleRegistry for the full set
	// of recognized names.
	ReEntryRuleName string `json:"reentry_rule"`
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

// StopFraction returns the fraction of the high-water mark the
// trailing stop retains — 1 - TrailingStopPercent (0.90 for EQS-01's
// own 10% reference configuration) — the value trailingStopExitRule
// actually multiplies the high-water mark by to compute the stop
// price. Only called by newTrailingStopExitRule; a Config selecting
// any other ExitRule never invokes this.
func (c Config) StopFraction() (num.Rate, error) {
	one := num.MustParseRate("1")
	retain, err := one.Sub(c.TrailingStopPercent)
	if err != nil {
		return num.Rate{}, fmt.Errorf("smatrend: computing stop retention fraction: %w", err)
	}
	return retain, nil
}

// Validate reports whether c is well-formed: SMAPeriod positive,
// ExitRuleName/ReEntryRuleName (or their defaults) recognized, and —
// only when the selected ExitRule is "trailing-stop" — TrailingStopPercent
// strictly between 0 and 1.
func (c Config) Validate() error {
	if c.SMAPeriod <= 0 {
		return fmt.Errorf("smatrend: sma period must be positive, got %d", c.SMAPeriod)
	}
	if _, ok := exitRuleRegistry[c.exitRuleName()]; !ok {
		return fmt.Errorf("smatrend: unknown exit_rule %q", c.ExitRuleName)
	}
	if _, ok := reEntryRuleRegistry[c.reEntryRuleName()]; !ok {
		return fmt.Errorf("smatrend: unknown reentry_rule %q", c.ReEntryRuleName)
	}
	if c.exitRuleName() == "trailing-stop" {
		if c.TrailingStopPercent.Sign() <= 0 {
			return fmt.Errorf("smatrend: trailing stop percent must be positive, got %s", c.TrailingStopPercent)
		}
		if c.TrailingStopPercent.Cmp(num.MustParseRate("1")) >= 0 {
			return fmt.Errorf("smatrend: trailing stop percent must be less than 1, got %s", c.TrailingStopPercent)
		}
	}
	return nil
}
