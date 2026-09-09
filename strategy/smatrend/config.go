package smatrend

import (
	"fmt"

	"github.com/rustyeddy/trader/num"
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
	// 90% of the high-water mark. It must be strictly between 0 and 1:
	// zero would place the stop exactly at the high-water mark itself
	// (triggering on the very next downtick), and 1 or more would place
	// it at or below zero.
	TrailingStopPercent num.Rate `json:"trailing_stop_percent"`
}

// StopFraction returns the fraction of the high-water mark the
// trailing stop retains — 1 - TrailingStopPercent (0.90 for EQS-01's
// own 10% reference configuration) — the value Strategy actually
// multiplies the high-water mark by to compute the stop price.
func (c Config) StopFraction() (num.Rate, error) {
	one := num.MustParseRate("1")
	retain, err := one.Sub(c.TrailingStopPercent)
	if err != nil {
		return num.Rate{}, fmt.Errorf("smatrend: computing stop retention fraction: %w", err)
	}
	return retain, nil
}

// Validate reports whether c is well-formed: SMAPeriod positive, and
// TrailingStopPercent strictly between 0 and 1.
func (c Config) Validate() error {
	if c.SMAPeriod <= 0 {
		return fmt.Errorf("smatrend: sma period must be positive, got %d", c.SMAPeriod)
	}
	if c.TrailingStopPercent.Sign() <= 0 {
		return fmt.Errorf("smatrend: trailing stop percent must be positive, got %s", c.TrailingStopPercent)
	}
	if c.TrailingStopPercent.Cmp(num.MustParseRate("1")) >= 0 {
		return fmt.Errorf("smatrend: trailing stop percent must be less than 1, got %s", c.TrailingStopPercent)
	}
	return nil
}
