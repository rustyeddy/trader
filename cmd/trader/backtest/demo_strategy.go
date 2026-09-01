package backtest

import (
	"context"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
)

// demoStrategy is a provisional, composition-side reference strategy
// (issue #222 review) that exists only so "trader backtest run" has
// something real to execute end to end — it is not the beginning of a
// strategy library or registry, and is not exported. It enters long
// once per requested instrument, on that instrument's own first bar,
// and never trades that instrument again: enough to genuinely
// exercise sizing, execution planning, risk admission, and simulated
// order submission/fill (the acceptance criterion's "-> M4 pipeline ->
// simulator" portion), without any pretense of being a real trading
// strategy. strategy.Strategy discovery/naming (a "strategy registry")
// is explicitly deferred — see ADR-039's own note and this issue's
// plan comment — this is the only strategy "trader backtest run" can
// select today.
//
// Issue #224 extended this from a single instrument to a portfolio of
// them: one demoStrategy instance still runs inside one Scheduler
// (never a per-symbol engine), tracking each instrument's own
// warm-up/entry state independently by instrument.ID string, the same
// per-instrument-keyed pattern backtest's own enterThenExitStrategy
// test fixture (#223) already established as correct.
type demoStrategy struct {
	instrumentIDs []instrument.ID
	interval      marketdata.Interval
	warmupBars    int

	intents strategy.IntentFactory
	seen    map[string]int
	entered map[string]bool
}

func newDemoStrategy(instrumentIDs []instrument.ID, interval marketdata.Interval, warmupBars int) *demoStrategy {
	return &demoStrategy{instrumentIDs: instrumentIDs, interval: interval, warmupBars: warmupBars}
}

func (s *demoStrategy) Describe() strategy.Descriptor {
	requirements := make([]strategy.DataRequirement, len(s.instrumentIDs))
	for i, instID := range s.instrumentIDs {
		requirements[i] = strategy.DataRequirement{Instrument: instID, Interval: s.interval, WarmupBars: s.warmupBars}
	}
	return strategy.Descriptor{
		Name:         "cli-demo",
		Version:      "v0",
		Requirements: requirements,
	}
}

func (s *demoStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	s.seen = make(map[string]int, len(s.instrumentIDs))
	s.entered = make(map[string]bool, len(s.instrumentIDs))
	return nil
}

// OnBar is called once per closed bar per instrument, including every
// one of Scheduler's own WarmupBars warm-up bars — Scheduler discards
// whatever intents it returns during warm-up itself; OnBar is never
// suppressed (issue #240 review: an earlier version of this method
// set entered=true on its very first callback, spending its one entry
// on a warm-up bar whose intent Scheduler would discard, so with
// --warmup-bars > 0 the strategy silently never traded again). The
// (warmupBars+1)th call for a given instrument — seen == warmupBars,
// 0-indexed — is the first one whose intents Scheduler actually honors
// for that instrument (allWarm's own "strictly more than WarmupBars
// closed bars" rule), so that is exactly when this strategy enters
// that instrument. nextBarOpenAfterEntry (run.go) must stay aligned
// with this same rule, per instrument. Each instrument's own
// seen/entered state is independent — one instrument reaching its
// entry bar never affects another's.
func (s *demoStrategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	key := event.Instrument.String()
	if s.entered[key] {
		return nil, nil
	}
	if s.seen[key] < s.warmupBars {
		s.seen[key]++
		return nil, nil
	}
	s.entered[key] = true

	in, err := s.intents.Enter(event.Instrument, order.Buy)
	if err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}
