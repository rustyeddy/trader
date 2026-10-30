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

func (bt *backtest) run(ctx context.Context, bars iter.Seq2[bar, error], strat strategy, h intentHandler) error {
	var last bar // most recent bar processed, for error context
	for b, err := range bars {
		if err != nil {
			if last.instrument == "" {
				return fmt.Errorf("reading bars: %w", err)
			}
			return fmt.Errorf("reading bars after %s %s: %w", last.instrument, last.Time.Format(time.DateOnly), err)
		}
		last = b
		intent := strat.onBar(ctx, b)
		if err := intent.validate(); err != nil {
			// A strategy bug, not a trading outcome: stop before the handler.
			return fmt.Errorf("strategy intent at %s %s: %w", b.instrument, b.Time.Format(time.DateOnly), err)
		}
		err := h.handleIntent(ctx, intent)
		switch {
		case errors.Is(err, errInsufficientMargin):
			// Expected trading outcome: log it and keep processing bars.
			slog.Warn("intent rejected",
				"reason", err,
				"time", b.Time,
				"instrument", intent.instrument,
				"action", intent.action,
				"direction", intent.direction)
		case err != nil:
			return fmt.Errorf("handling %s %s at %s %s: %w",
				intent.action, intent.instrument, b.instrument, b.Time.Format(time.DateOnly), err)
		}
	}
	return nil
}

type result struct {
}

func (r *result) writeJournal() error {
	return nil
}
