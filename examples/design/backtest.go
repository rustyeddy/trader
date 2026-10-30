package main

import (
	"context"
	"errors"
	"iter"
	"log/slog"
)

type backtest struct {
}

func (bt *backtest) run(ctx context.Context, bars iter.Seq2[ohlc, error], strat strategy, h intentHandler) error {
	for bar, err := range bars {
		if err != nil {
			return err
		}
		intent := strat.onBar(ctx, bar)
		err := h.handleIntent(ctx, intent)
		switch {
		case errors.Is(err, errInsufficientMargin):
			// Expected trading outcome: log it and keep processing bars.
			slog.Warn("intent rejected",
				"reason", err,
				"instrument", intent.instrument,
				"action", intent.action,
				"direction", intent.direction)
		case err != nil:
			return err
		}
	}
	return nil
}

type result struct {
}

func (r *result) writeJournal() error {
	return nil
}
