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

func (s *demoStrategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	if s.entered {
		return nil, nil
	}
	s.entered = true

	in, err := s.intents.Enter(event.Instrument, order.Buy)
	if err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}
