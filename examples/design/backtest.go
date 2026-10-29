package main

import (
	"context"
	"iter"
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
		if err != nil {
			return err // Question: HOW to handle these errors?
		}
	}
	return nil
}

type result struct {
}

func (r *result) writeJournal() error {
	return nil
}
