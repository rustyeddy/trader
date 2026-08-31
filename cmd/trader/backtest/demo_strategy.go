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
// once, on the instrument's own first bar, and never trades again:
// enough to genuinely exercise sizing, execution planning, risk
// admission, and simulated order submission/fill (the acceptance
// criterion's "-> M4 pipeline -> simulator" portion), without any
// pretense of being a real trading strategy. strategy.Strategy
// discovery/naming (a "strategy registry") is explicitly deferred —
// see ADR-039's own note and this issue's plan comment — this is the
// only strategy "trader backtest run" can select today.
type demoStrategy struct {
	instrumentID instrument.ID
	interval     marketdata.Interval
	warmupBars   int

	intents strategy.IntentFactory
	seen    int
	entered bool
}

func newDemoStrategy(instrumentID instrument.ID, interval marketdata.Interval, warmupBars int) *demoStrategy {
	return &demoStrategy{instrumentID: instrumentID, interval: interval, warmupBars: warmupBars}
}

func (s *demoStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{
		Name:    "cli-demo",
		Version: "v0",
		Requirements: []strategy.DataRequirement{
			{Instrument: s.instrumentID, Interval: s.interval, WarmupBars: s.warmupBars},
		},
	}
}

func (s *demoStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	return nil
}

// OnBar is called once per closed bar, including every one of
// Scheduler's own WarmupBars warm-up bars — Scheduler discards
// whatever intents it returns during warm-up itself; OnBar is never
// suppressed (issue #240 review: an earlier version of this method
// set entered=true on its very first callback, spending its one entry
// on a warm-up bar whose intent Scheduler would discard, so with
// --warmup-bars > 0 the strategy silently never traded again). The
// (warmupBars+1)th call — s.seen == s.warmupBars, 0-indexed — is the
// first one whose intents Scheduler actually honors (allWarm's own
// "strictly more than WarmupBars closed bars" rule), so that is
// exactly when this strategy enters. nextBarOpenAfterEntry (run.go)
// must stay aligned with this same rule.
func (s *demoStrategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	if s.entered {
		return nil, nil
	}
	if s.seen < s.warmupBars {
		s.seen++
		return nil, nil
	}
	s.entered = true

	in, err := s.intents.Enter(event.Instrument, order.Buy)
	if err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}
