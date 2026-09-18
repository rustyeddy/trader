// Command strategysdk-minimal is the minimal out-of-tree strategy
// binary issue #381's own acceptance criteria ask for: it compiles
// and serves Strategy Protocol v1 over a Unix-domain socket using
// only strategysdk — never importing Trader's own strategy, backtest,
// or adapters packages (strategysdk/boundary_test.go enforces the
// identical rule for strategysdk itself; this example demonstrates
// the same discipline from an author's own, separate perspective).
//
// It implements a deliberately trivial "flip-flop" strategy: flat on
// its first bar, it enters long; once long, the next bar exits. This
// is not a trading strategy anyone should run for real — it exists to
// show the complete shape of a strategysdk.Strategy (Describe/Start/
// OnBar) and the one-line Serve() a real author's own main() needs,
// nothing more.
//
// See this directory's own README.md for how to launch it against a
// Trader host and what environment variable it expects.
package main

import (
	"context"
	"log"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategysdk"
)

func main() {
	if err := strategysdk.Serve(newFlipFlop()); err != nil {
		log.Fatal(err)
	}
}

// flipFlop is the smallest useful strategysdk.Strategy: it never
// consults history or the account snapshot in any real way, and it
// implements no optional capability (no FillHandler) — see
// strategysdk's own doc comment for what each of Describe/Start/OnBar
// is for.
type flipFlop struct {
	instrument instrument.ID
	interval   marketdata.Interval
	long       bool
}

func newFlipFlop() *flipFlop {
	// EUR/USD, H1 — the same instrument/interval convention Trader's
	// own examples use elsewhere (examples/m1, examples/m5).
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	interval, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	if err != nil {
		// marketdata.NewInterval only fails for a malformed unit/count;
		// both are fixed, valid literals here, so this can never
		// actually happen — panic makes that assumption explicit
		// rather than silently constructing a zero-value strategy.
		panic(err)
	}
	return &flipFlop{instrument: inst, interval: interval}
}

func (f *flipFlop) Describe() strategysdk.Descriptor {
	return strategysdk.Descriptor{
		Name:    "flipflop",
		Version: "0.1.0",
		Requirements: []strategysdk.DataRequirement{
			{Instrument: f.instrument, Interval: f.interval, WarmupBars: 0},
		},
	}
}

func (f *flipFlop) Start(_ context.Context, env strategysdk.Environment) error {
	env.Logger.Info("flipflop starting", "run_id", env.RunID, "start", env.Clock.Now())
	return nil
}

func (f *flipFlop) OnBar(_ context.Context, event strategysdk.BarEvent, _ strategysdk.View) ([]strategysdk.DescribedIntent, []strategysdk.DescribedSignal, error) {
	if !event.Instrument.Equal(f.instrument) {
		return nil, nil, nil
	}

	if !f.long {
		f.long = true
		return []strategysdk.DescribedIntent{strategysdk.Enter(f.instrument, order.Buy)}, nil, nil
	}

	f.long = false
	return []strategysdk.DescribedIntent{strategysdk.Exit(f.instrument)}, nil, nil
}
