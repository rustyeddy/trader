package emacross

import "fmt"

// Config is this strategy's own typed configuration — fast/slow EMA
// periods supplied by the composition root, never a generic
// map[string]any (architecture document's own "parameters should be
// strongly typed inside a strategy" rule).
type Config struct {
	FastPeriod int
	SlowPeriod int
}

// Validate reports whether c is well-formed: both periods positive and
// SlowPeriod strictly greater than FastPeriod, matching
// docs/research/ema-01-experiment-definition.org's own reference
// values (20/50) and cmd/trader/backtest's identical validation for
// its own not-yet-connected strategy config (issue #247).
func (c Config) Validate() error {
	if c.FastPeriod <= 0 {
		return fmt.Errorf("emacross: fast period must be positive, got %d", c.FastPeriod)
	}
	if c.SlowPeriod <= c.FastPeriod {
		return fmt.Errorf("emacross: slow period (%d) must be greater than fast period (%d)", c.SlowPeriod, c.FastPeriod)
	}
	return nil
}
