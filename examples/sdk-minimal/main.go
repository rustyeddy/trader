// Command sdk-minimal is the minimal out-of-tree strategy
// binary issue #381's own acceptance criteria ask for: it compiles
// and serves Strategy Protocol v1 over a Unix-domain socket using
// only sdk — never importing Trader's own strategy, backtest,
// or adapters packages (sdk/boundary_test.go enforces the
// identical rule for sdk itself; this example demonstrates
// the same discipline from an author's own, separate perspective).
//
// It implements a deliberately trivial "flip-flop" strategy: when the
// account is flat it enters long; once long, the next bar exits. It
// decides from the account's actual position in its View, not from its
// own memory of what it asked for, because an entry can be refused
// (for example for insufficient initial margin, ADR-066). This
// is not a trading strategy anyone should run for real — it exists to
// show the complete shape of a sdk.Strategy (Describe/Start/
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
	"github.com/rustyeddy/trader/sdk"
)

func main() {
	if err := sdk.Serve(newFlipFlop()); err != nil {
		log.Fatal(err)
	}
}

// flipFlop is the smallest useful sdk.Strategy: it never consults
// history, reads only whether the account holds its instrument, and
// implements no optional capability (no FillHandler) — see sdk's own
// doc comment for what each of Describe/Start/OnBar is for.
type flipFlop struct {
	instrument instrument.ID
	interval   marketdata.Interval
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

func (f *flipFlop) Describe() sdk.Descriptor {
	return sdk.Descriptor{
		Name:    "flipflop",
		Version: "0.2.0",
		Requirements: []sdk.DataRequirement{
			{Instrument: f.instrument, Interval: f.interval, WarmupBars: 0},
		},
	}
}

func (f *flipFlop) Start(_ context.Context, env sdk.Environment) error {
	env.Logger.Info("flipflop starting", "run_id", env.RunID, "start", env.Clock.Now())
	return nil
}

func (f *flipFlop) OnBar(_ context.Context, event sdk.BarEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
	if !event.Instrument.Equal(f.instrument) {
		return nil, nil, nil
	}

	// Act on what the account actually holds: an entry this strategy
	// asked for may have been refused, and exiting a position that was
	// never opened is meaningless.
	if f.holding(view) {
		return []sdk.DescribedIntent{sdk.Exit(f.instrument)}, nil, nil
	}
	return []sdk.DescribedIntent{sdk.Enter(f.instrument, order.Buy)}, nil, nil
}

// holding reports whether the account has an open position in f's
// instrument.
func (f *flipFlop) holding(view sdk.View) bool {
	for _, p := range view.Account().Positions {
		if p.Instrument.Equal(f.instrument) && p.Side != order.Flat {
			return true
		}
	}
	return false
}
