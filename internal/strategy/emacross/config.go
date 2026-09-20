package emacross

import "fmt"

// Side restricts which position direction Strategy is allowed to
// hold (issue #273), letting the same crossover logic run in a
// directionally-restricted mode for research comparison — for
// example, isolating the short side's own performance — without
// duplicating strategy logic or approximating it by post-hoc
// filtering an existing mixed-direction run's trades (which would be
// wrong: fixed-fraction sizing means a true single-side run has its
// own, different equity trajectory the moment the first
// opposite-side trade is skipped).
//
// The zero value, SideBoth, trades both directions — EMA-01's own
// reference behavior — so an unconfigured Config (every existing
// caller before this issue) is unchanged.
type Side uint8

const (
	// SideBoth trades both directions: EMA-01's own reference
	// strategy, unchanged from before this option existed.
	SideBoth Side = iota
	// SideLongOnly never opens a short position. A bearish cross
	// while long closes the long (exit-only), rather than reversing
	// into a short.
	SideLongOnly
	// SideShortOnly never opens a long position. A bullish cross
	// while short closes the short (exit-only), rather than reversing
	// into a long.
	SideShortOnly
)

// String returns a human-readable Side name, and is what
// Side.MarshalText produces — so Config.AllowedSide serializes into
// Manifest.StrategyParameters (and any --format json/org report) as a
// readable string, e.g. "short-only", never a bare integer.
func (s Side) String() string {
	switch s {
	case SideBoth:
		return "both"
	case SideLongOnly:
		return "long-only"
	case SideShortOnly:
		return "short-only"
	default:
		return fmt.Sprintf("Side(%d)", uint8(s))
	}
}

func (s Side) valid() bool {
	switch s {
	case SideBoth, SideLongOnly, SideShortOnly:
		return true
	default:
		return false
	}
}

// allowsLong reports whether s permits opening or holding a long
// position.
func (s Side) allowsLong() bool { return s != SideShortOnly }

// allowsShort reports whether s permits opening or holding a short
// position.
func (s Side) allowsShort() bool { return s != SideLongOnly }

// MarshalText implements encoding.TextMarshaler, via String.
func (s Side) MarshalText() ([]byte, error) {
	if !s.valid() {
		return nil, fmt.Errorf("emacross: %s", s)
	}
	return []byte(s.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, so Side is
// directly usable as a config.Load-decoded field (matching
// num.Rate/num.Price's own pattern) from a YAML value, CLI flag, or
// environment variable — see cmd/trader/backtest's own
// strategySection.
func (s *Side) UnmarshalText(text []byte) error {
	switch string(text) {
	case "both", "":
		*s = SideBoth
	case "long-only":
		*s = SideLongOnly
	case "short-only":
		*s = SideShortOnly
	default:
		return fmt.Errorf("emacross: invalid allowed_side %q: expected one of both, long-only, short-only", text)
	}
	return nil
}

// Config is this strategy's own typed configuration — fast/slow EMA
// periods supplied by the composition root, never a generic
// map[string]any (architecture document's own "parameters should be
// strongly typed inside a strategy" rule).
//
// JSON tags follow the snake_case convention backtest/manifest_test.go
// already establishes for strategy parameters (PR #260 review): a
// composition root can pass Config straight through as
// backtest.ManifestParams.StrategyParameters, which NewManifest
// canonically marshals via json.Marshal, and get "fast_period"/
// "slow_period" keys rather than Go's default "FastPeriod"/
// "SlowPeriod".
type Config struct {
	FastPeriod int `json:"fast_period"`
	SlowPeriod int `json:"slow_period"`
	// AllowedSide restricts which position direction this Strategy is
	// allowed to hold (issue #273). The zero value, SideBoth, is
	// EMA-01's own reference behavior.
	AllowedSide Side `json:"allowed_side"`
}

// Validate reports whether c is well-formed: both periods positive and
// SlowPeriod strictly greater than FastPeriod, matching
// docs/research/ema-01-experiment-definition.org's own reference
// values (20/50) and cmd/trader/backtest's identical validation for
// its own not-yet-connected strategy config (issue #247); AllowedSide
// must be one of its three defined values.
func (c Config) Validate() error {
	if c.FastPeriod <= 0 {
		return fmt.Errorf("emacross: fast period must be positive, got %d", c.FastPeriod)
	}
	if c.SlowPeriod <= 0 {
		return fmt.Errorf("emacross: slow period must be positive, got %d", c.SlowPeriod)
	}
	if c.SlowPeriod <= c.FastPeriod {
		return fmt.Errorf("emacross: slow period (%d) must be greater than fast period (%d)", c.SlowPeriod, c.FastPeriod)
	}
	if !c.AllowedSide.valid() {
		return fmt.Errorf("emacross: invalid allowed_side %s", c.AllowedSide)
	}
	return nil
}
