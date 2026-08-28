package strategy

import (
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

// Descriptor identifies one strategy and states what data it needs
// before it can be run. Kept narrow for v0 (issue #210's own review):
// ParameterSchema and StateVersion are not added until #215 (run
// manifest) or #218 (journal/state) shows exactly what they need to
// carry — a strategy's own parameters remain strongly-typed
// configuration passed when the concrete Strategy value is
// constructed, never a generic runtime parameter bag.
type Descriptor struct {
	// Name identifies this strategy, for example "ema_cross". Used as
	// the Source on every order.Intent the strategy's own
	// IntentFactory builds (see Environment).
	Name string
	// Version distinguishes revisions of the same strategy — for
	// example when its logic changes in a way that would make two runs
	// incomparable.
	Version string
	// Requirements states the market data this strategy needs before
	// its first OnBar call, so a runner can validate the environment
	// (and complete any warm-up) ahead of time rather than discovering
	// a missing requirement mid-run.
	Requirements []DataRequirement
}

// DataRequirement names one instrument/interval a strategy needs bars
// for, and how many bars of warm-up history it needs before its first
// decision is meaningful.
type DataRequirement struct {
	// Instrument is the canonical instrument identity required.
	Instrument instrument.ID
	// Interval is the bar interval required for Instrument.
	Interval marketdata.Interval
	// WarmupBars is how many bars of history before the run's own
	// start time this requirement needs replayed (but not decided on)
	// before OnBar is first called for real.
	WarmupBars int
}
