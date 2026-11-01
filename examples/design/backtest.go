package main

import (
	"context"
	"fmt"
	"iter"
	"time"
)

type backtest struct {
}

func (bt *backtest) run(ctx context.Context, bars iter.Seq2[bar, error], tr *trader) error {
	var last bar // most recent bar processed, for error context
	for b, err := range bars {
		if err != nil {
			if last.instrument == "" {
				return fmt.Errorf("reading bars: %w", err)
			}
			return fmt.Errorf("reading bars after %s %s: %w", last.instrument, last.Time.Format(time.DateOnly), err)
		}
		last = b

		if err := tr.onBar(ctx, b); err != nil {
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
