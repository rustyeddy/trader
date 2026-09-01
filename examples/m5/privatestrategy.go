// Package m5 contains a representative "private strategy" — a
// strategy.Strategy implementation written the way real code living
// outside this module would write one (issue #225, M5-17). It is not
// itself a separate Go module (a genuinely external repository is not
// meaningfully different from this file for the purpose of this
// proof, and keeping it in-tree lets boundary_test.go and
// privatestrategy_test.go mechanically verify the property rather than
// merely assert it), but its own imports are deliberately restricted
// to exactly the public contract strategy/doc.go documents, plus the
// standard library's context: context, order, marketdata, instrument,
// and strategy itself. It never
// imports backtest, service, cmd, or adapters — see boundary_test.go
// for the mechanical guard, and privatestrategy_test.go for the
// composition-root glue (which legitimately does import those
// packages, the same way a real application built on top of a private
// strategy would) that actually runs it through backtest.Runner via
// service/backtest.Service.
//
// privateStrategy itself does nothing sophisticated: it enters long
// once on its one required instrument's own first bar and never
// trades again — deliberately as simple as cmd/trader/backtest's own
// demoStrategy (#222), since the property this package exists to
// prove is the *boundary*, not trading logic.
package m5

import (
	"context"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
)

// PrivateStrategy is the representative external strategy
// implementation. It is exported (unlike cmd/trader/backtest's own
// unexported demoStrategy) specifically so privatestrategy_test.go —
// standing in for a separate application built on top of a private
// strategy — can construct it using nothing but this package's own
// public API, the way a real external consumer would.
type PrivateStrategy struct {
	instrumentID instrument.ID
	interval     marketdata.Interval

	intents strategy.IntentFactory
	entered bool
}

// NewPrivateStrategy returns a PrivateStrategy that trades instrumentID
// on the given interval.
func NewPrivateStrategy(instrumentID instrument.ID, interval marketdata.Interval) *PrivateStrategy {
	return &PrivateStrategy{instrumentID: instrumentID, interval: interval}
}

// Describe implements strategy.Strategy.
func (s *PrivateStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{
		Name:    "private-example",
		Version: "v0",
		Requirements: []strategy.DataRequirement{
			{Instrument: s.instrumentID, Interval: s.interval},
		},
	}
}

// Start implements strategy.Strategy.
func (s *PrivateStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	return nil
}

// OnBar implements strategy.Strategy.
func (s *PrivateStrategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
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
