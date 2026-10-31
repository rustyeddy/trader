package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"time"
)

type backtest struct {
}

func (bt *backtest) run(ctx context.Context, bars iter.Seq2[bar, error], strat strategy, h orderHandler) error {
	var last bar // most recent bar processed, for error context
	for b, err := range bars {
		if err != nil {
			if last.instrument == "" {
				return fmt.Errorf("reading bars: %w", err)
			}
			return fmt.Errorf("reading bars after %s %s: %w", last.instrument, last.Time.Format(time.DateOnly), err)
		}
		last = b

		o := strat.onBar(ctx, b)
		if o == nil {
			continue // nothing to do on this bar
		}
		if err := o.validate(); err != nil {
			// A strategy bug, not a trading outcome: stop before the handler.
			return fmt.Errorf("strategy order at %s %s: %w", b.instrument, b.Time.Format(time.DateOnly), err)
		}

		o.record(stateRequested, "")
		err := h.handleOrder(ctx, o)
		switch {
		case errors.Is(err, errInsufficientMargin):
			// Expected trading outcome: record it, log it and keep processing bars.
			if o.state != stateRejected { // the rejecting step normally records it
				o.record(stateRejected, err.Error())
			}
			slog.Warn("order rejected",
				"reason", err,
				"time", b.Time,
				"instrument", o.instrument,
				"action", o.action,
				"direction", o.direction)
		case err != nil:
			return fmt.Errorf("handling %s %s at %s %s: %w",
				o.action, o.instrument, b.instrument, b.Time.Format(time.DateOnly), err)
		}
	}
	return nil
}

type result struct {
}

func (r *result) writeJournal() error {
	return nil
}
