package main

import (
	"context"
	"iter"
)

type backtest struct {
}

func (bt *backtest) run(ctx context.Context, bars iter.Seq2[ohlc, error], strat strategy, tr *trader) error {
	for bar, err := range bars {
		if err != nil {
			return err
		}
		intent := strat.onBar(ctx, bar)
		tr.handleIntent(ctx, intent)
	}
	return nil
}

type result struct {
}

func (r *result) writeJournal() error {
	return nil
}
