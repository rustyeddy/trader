package backtest

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/internal/strategies"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
)

// TickFeed yields broker.Price rows (typically from a dataset) one at a time.
// Implementations should be deterministic and return (ok=false, err=nil) at EOF.
type TickFeed interface {
	Next() (p broker.Price, ok bool, err error)
	Close() error
}

// RunnerOptions controls how the generic backtest runner behaves.
type RunnerOptions struct {
	// If true, close all open positions at the end of the dataset.
	// Close reason will be CloseReason (or "EndOfReplay" if empty).
	CloseEnd    bool
	CloseReason string
}

// Runner drives an engine forward using a feed and strategy.
type Runner struct {
	Engine   *sim.Engine
	Feed     TickFeed
	Strategy strategies.TickStrategy
	Options  RunnerOptions
}

// Run executes the backtest loop:
//  1. read next tick
//  2. engine.UpdatePrice(tick)
//  3. strategy.OnTick(ctx, engine, tick)
//
// If j is not nil, a simple trades/wins/losses summary is computed from the journal
// over the observed dataset time range.
func (r *Runner) Run(ctx context.Context, j *journal.SQLiteJournal) (Result, error) {
	if r.Engine == nil {
		return Result{}, fmt.Errorf("backtest: Engine is required")
	}
	if r.Feed == nil {
		return Result{}, fmt.Errorf("backtest: Feed is required")
	}
	if r.Strategy == nil {
		return Result{}, fmt.Errorf("backtest: Strategy is required")
	}
	defer r.Feed.Close()

	var start, end time.Time

	for {
		p, ok, err := r.Feed.Next()
		if err != nil {
			return Result{}, err
		}
		if !ok {
			break
		}

		if start.IsZero() || p.Time.Before(start) {
			start = p.Time
		}
		if end.IsZero() || p.Time.After(end) {
			end = p.Time
		}

		if err := r.Engine.UpdatePrice(p); err != nil {
			return Result{}, err
		}
		if err := r.Strategy.OnTick(ctx, r.Engine, p); err != nil {
			return Result{}, err
		}
	}

	if r.Options.CloseEnd {
		reason := r.Options.CloseReason
		if reason == "" {
			reason = "EndOfReplay"
		}
		_ = r.Engine.CloseAll(ctx, reason)
	}

	acct, _ := r.Engine.GetAccount(ctx)

	wins := 0
	losses := 0
	trades := 0

	if j != nil && !start.IsZero() && !end.IsZero() && start.Before(end) {
		// include trades that close exactly at end by extending window slightly
		recs, err := j.ListTradesClosedBetween(start, end.Add(time.Nanosecond))
		if err == nil {
			trades = len(recs)
			for _, tr := range recs {
				if tr.RealizedPL > 0 {
					wins++
				} else if tr.RealizedPL < 0 {
					losses++
				}
			}
		}
	}

	return Result{
		Balance: acct.Balance,
		Equity:  acct.Equity,
		Trades:  trades,
		Wins:    wins,
		Losses:  losses,
		Start:   start,
		End:     end,
	}, nil
}
