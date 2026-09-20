// Package m5 contains a representative in-tree private/runtime strategy
// (issue #225, M5-17). It demonstrates how Trader's own composition root can
// wire an internal strategy into the backtest runtime. Its imports deliberately
// use internal/strategy and internal/order, so this example is not an external
// consumer boundary test. External strategy authors should use sdk and the
// separate-module checks in test/architecture instead.
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
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/order"
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
func (s *PrivateStrategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]runtimeorder.Intent, error) {
	if s.entered {
		return nil, nil
	}
	s.entered = true

	in, err := s.intents.Enter(event.Instrument, order.Buy)
	if err != nil {
		return nil, err
	}
	return []runtimeorder.Intent{in}, nil
}
