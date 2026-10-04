package backtestcfg

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// nextBarOpenAfterEntry returns the Open of the bar immediately
// following demoStrategy's own entry bar for instrumentID — the exact
// price Scheduler's next-bar-open fill-eligibility rule (issue #214)
// actually fills a market order at, never the entry bar's own Close
// (PR #240 review). Scheduler calls Strategy.OnBar for every bar,
// including each of the first warmupBars warm-up bars — it discards
// whatever intent OnBar returns during warm-up itself, it does not
// suppress the call (PR #240 second-review correction; demo_strategy.go's
// own doc comment records the same rule). demoStrategy tracks these
// callbacks itself and deliberately withholds its Enter intent until
// callback/bar index warmupBars — the first one Scheduler actually
// honors — so that bar is the entry bar, and the fill bar is the one
// immediately after it, index warmupBars+1. This function reads and
// discards exactly warmupBars+1 bars before returning the following
// bar's Open. Each instrument's own entry/fill bar is computed
// independently, since demoStrategy enters each instrument on that
// instrument's own first bar, not a shared portfolio-wide bar index.
func nextBarOpenAfterEntry(ctx context.Context, manager *marketruntime.Manager, instrumentID instrument.ID, interval marketdata.Interval, span marketdata.TimeRange, warmupBars int) (num.Price, error) {
	reader, err := manager.Bars(ctx, marketruntime.BarQuery{Instrument: instrumentID, Interval: interval, Range: span})
	if err != nil {
		return num.Price{}, err
	}
	defer func() { _ = reader.Close() }()

	// Discard the warmupBars warm-up bars plus the entry bar itself
	// (warmupBars + 1 bars total), then the next Next() call returns
	// the fill bar.
	for i := 0; i < warmupBars+1; i++ {
		if _, err := reader.Next(ctx); err != nil {
			return num.Price{}, fmt.Errorf("not enough bars in the requested range for the demo strategy to enter (need at least %d before its fill bar): %w", warmupBars+1, err)
		}
	}

	fillBar, err := reader.Next(ctx)
	if err != nil {
		return num.Price{}, fmt.Errorf("not enough bars in the requested range for the demo strategy's entry to fill on the following bar: %w", err)
	}
	return fillBar.Open, nil
}
